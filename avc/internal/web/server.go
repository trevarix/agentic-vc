package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/api"
	branchpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
	mergepkg "github.com/SkillMythOrg/agentic-vc/avc/internal/merge"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
)

// Serve starts the HTTP server. Blocks until the server stops.
func Serve(addr, projectPath string) error {
	mux := http.NewServeMux()

	// Static assets (embedded into the binary).
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("static FS: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// API endpoints — snapshots.
	mux.HandleFunc("/api/project", projectInfoHandler(projectPath))
	mux.HandleFunc("/api/snapshots", listSnapshotsHandler(projectPath))
	mux.HandleFunc("/api/snapshots/create", createSnapshotHandler(projectPath))
	mux.HandleFunc("/api/snapshots/", snapshotByIDHandler(projectPath))
	mux.HandleFunc("/api/diff", diffHandler(projectPath))
	mux.HandleFunc("/api/diff-current", diffCurrentHandler(projectPath))
	mux.HandleFunc("/api/restore", restoreHandler(projectPath))
	mux.HandleFunc("/api/restore-file", restoreFileHandler(projectPath))

	// API endpoints — branches.
	// Note: /api/branches/switch must be registered before /api/branches/ so the
	// more-specific pattern wins in Go's default mux.
	mux.HandleFunc("/api/branches/switch", branchSwitchHandler(projectPath))
	mux.HandleFunc("/api/branches/", branchByNameHandler(projectPath))
	mux.HandleFunc("/api/branches", branchesHandler(projectPath))

	// API endpoints — merge.
	mux.HandleFunc("/api/merge/preview", mergePreviewHandler(projectPath))
	mux.HandleFunc("/api/merge/abort", mergeAbortHandler(projectPath))
	mux.HandleFunc("/api/merge", mergeHandler(projectPath))

	// API endpoints — status & storage.
	mux.HandleFunc("/api/status", statusHandler(projectPath))
	mux.HandleFunc("/api/storage", storageHandler(projectPath))

	srv := &http.Server{Addr: addr, Handler: mux}
	return srv.ListenAndServe()
}

// ─── helpers ────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func snapshotToMap(s *db.Snapshot) map[string]any {
	return map[string]any{
		"id":            s.ID,
		"label":         s.Label,
		"timestamp":     s.Timestamp,
		"agent_name":    s.AgentName,
		"files_changed": s.FileCount,
		"total_size":    s.TotalSize,
		"notes":         s.Notes,
		"branch_id":     s.BranchID,
	}
}

func branchToMap(b *db.Branch, projectPath string) map[string]any {
	ws := branchpkg.WorkspacePath(projectPath, b.Name)
	return map[string]any{
		"id":               b.ID,
		"name":             b.Name,
		"base_snapshot_id": b.BaseSnapshotID,
		"workspace":        ws,
	}
}

func diffFilesToMap(files []*diff.FileDiff) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, f := range files {
		out[i] = map[string]any{
			"path":          f.Path,
			"type":          string(f.Type),
			"old_hash":      f.OldHash,
			"new_hash":      f.NewHash,
			"lines_added":   f.LinesAdded,
			"lines_removed": f.LinesRemoved,
			"diff_preview":  f.DiffPreview,
		}
	}
	return out
}

func formatDiffResult(result *diff.Result, from, to string) map[string]any {
	return map[string]any{
		"from_snapshot": from,
		"to_snapshot":   to,
		"files":         diffFilesToMap(result.Files),
	}
}

func mergeResultToMap(result *mergepkg.Result) map[string]any {
	files := make([]map[string]any, len(result.Files))
	for i, f := range result.Files {
		files[i] = map[string]any{
			"path":        f.Path,
			"decision":    f.Decision,
			"base_hash":   f.BaseHash,
			"main_hash":   f.MainHash,
			"branch_hash": f.BranchHash,
		}
	}
	m := map[string]any{
		"merge_id":    result.MergeID,
		"branch_name": result.BranchName,
		"conflicts":   result.Conflicts,
		"clean":       result.Clean,
		"skipped":     result.Skipped,
		"files":        files,
	}
	if result.PostMergeSnapshotID != "" {
		m["post_merge_snapshot_id"] = result.PostMergeSnapshotID
	}
	return m
}

// ─── snapshot handlers ───────────────────────────────────────────────────────

func projectInfoHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		branchName := branchpkg.GetActiveBranchName(projectPath)
		writeJSON(w, http.StatusOK, map[string]any{
			"path":          projectPath,
			"name":          filepath.Base(projectPath),
			"active_branch": branchName,
		})
	}
}

func listSnapshotsHandler(projectPath string) http.HandlerFunc {
	ops := api.SnapshotOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		q := r.URL.Query()
		f := db.SnapshotFilter{
			Query:     q.Get("q"),
			AgentName: q.Get("agent"),
			Tag:       q.Get("tag"),
			FilePath:  q.Get("changed"),
			Limit:     -1, // web UI: return all (frontend handles display limits)
		}
		snapshots, err := ops.List(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, len(snapshots))
		for i, s := range snapshots {
			out[i] = snapshotToMap(s)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type createSnapshotRequest struct {
	Label string `json:"label"`
	Agent string `json:"agent"`
	Notes string `json:"notes"`
}

func createSnapshotHandler(projectPath string) http.HandlerFunc {
	ops := api.SnapshotOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req createSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Label == "" {
			writeError(w, http.StatusBadRequest, "label is required")
			return
		}
		snap, err := ops.Create(req.Label, req.Agent, req.Notes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":            snap.ID,
			"label":         snap.Label,
			"timestamp":     snap.Timestamp,
			"agent_name":    snap.AgentName,
			"files_changed": snap.FileCount,
			"total_size":    snap.TotalSize,
			"notes":         snap.Notes,
			"branch_id":     snap.BranchID,
			"success":       true,
		})
	}
}

// snapshotByIDHandler routes /api/snapshots/<id> for GET (info) and DELETE.
func snapshotByIDHandler(projectPath string) http.HandlerFunc {
	ops := api.SnapshotOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "snapshot id required")
			return
		}
		switch r.Method {
		case http.MethodGet:
			snap, files, err := ops.Info(id)
			if err != nil {
				writeError(w, http.StatusNotFound, fmt.Sprintf("snapshot '%s' not found", id))
				return
			}
			fileList := make([]map[string]any, len(files))
			for i, f := range files {
				fileList[i] = map[string]any{
					"path": f.RelativePath,
					"hash": f.FileHash,
					"size": f.FileSize,
				}
			}
			out := snapshotToMap(snap)
			out["file_count"] = len(files)
			out["files"] = fileList
			writeJSON(w, http.StatusOK, out)
		case http.MethodDelete:
			if err := ops.Delete(id); err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "success": true})
		default:
			methodNotAllowed(w)
		}
	}
}

func diffHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if from == "" || to == "" {
			writeError(w, http.StatusBadRequest, "from and to query parameters are required")
			return
		}
		result, err := diff.Compare(projectPath, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, formatDiffResult(result, from, to))
	}
}

func diffCurrentHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id query parameter is required")
			return
		}
		result, err := diff.CompareWithCurrent(projectPath, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, formatDiffResult(result, id, "working-tree"))
	}
}

type restoreRequest struct {
	ID string `json:"id"`
}

func restoreHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req restoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "id is required")
			return
		}
		result, err := restore.Restore(projectPath, req.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":             result.SnapshotID,
			"restored_files": result.RestoredFiles,
			"restored_size":  result.RestoredSize,
			"success":        true,
		})
	}
}

type restoreFileRequest struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func restoreFileHandler(projectPath string) http.HandlerFunc {
	ops := api.SnapshotOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req restoreFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ID == "" || req.Path == "" {
			writeError(w, http.StatusBadRequest, "id and path are required")
			return
		}
		result, err := ops.RestoreFile(req.ID, req.Path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        result.SnapshotID,
			"file_path": result.FilePath,
			"size":      result.Size,
			"success":   true,
		})
	}
}

// ─── branch handlers ─────────────────────────────────────────────────────────

// branchesHandler handles GET /api/branches (list) and POST /api/branches (create).
func branchesHandler(projectPath string) http.HandlerFunc {
	ops := api.BranchOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			res, err := ops.List()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out := make([]map[string]any, len(res.Branches))
			for i, b := range res.Branches {
				m := branchToMap(b, projectPath)
				m["active"] = b.Name == res.ActiveName
				out[i] = m
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"branches":    out,
				"active_name": res.ActiveName,
			})

		case http.MethodPost:
			var req struct {
				Name           string `json:"name"`
				FromSnapshotID string `json:"from_snapshot_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			if req.Name == "" {
				writeError(w, http.StatusBadRequest, "name is required")
				return
			}
			b, ws, err := ops.Create(req.Name, req.FromSnapshotID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out := branchToMap(b, projectPath)
			out["workspace"] = ws
			out["success"] = true
			writeJSON(w, http.StatusOK, out)

		default:
			methodNotAllowed(w)
		}
	}
}

// branchSwitchHandler handles POST /api/branches/switch.
func branchSwitchHandler(projectPath string) http.HandlerFunc {
	ops := api.BranchOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := ops.Switch(req.Name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"active_branch": req.Name,
			"success":       true,
		})
	}
}

// branchByNameHandler handles:
//
//	DELETE /api/branches/:name         — delete branch
//	GET    /api/branches/:name/diff    — cumulative diff
func branchByNameHandler(projectPath string) http.HandlerFunc {
	ops := api.BranchOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		// Strip the /api/branches/ prefix.
		rest := strings.TrimPrefix(r.URL.Path, "/api/branches/")
		if rest == "" {
			writeError(w, http.StatusBadRequest, "branch name required")
			return
		}

		// GET /api/branches/:name/diff
		if strings.HasSuffix(rest, "/diff") {
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			name := strings.TrimSuffix(rest, "/diff")
			result, err := ops.Diff(name)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"branch_name":      result.BranchName,
				"from_snapshot_id": result.FromSnapshotID,
				"to_snapshot_id":   result.ToSnapshotID,
				"files":            diffFilesToMap(result.Diff.Files),
			})
			return
		}

		// DELETE /api/branches/:name
		if r.Method == http.MethodDelete {
			name := rest
			keepHistory := r.URL.Query().Get("keep_history") == "true"
			if err := ops.Delete(name, keepHistory); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"name":    name,
				"success": true,
			})
			return
		}

		methodNotAllowed(w)
	}
}

// ─── merge handlers ──────────────────────────────────────────────────────────

// mergePreviewHandler handles GET /api/merge/preview?branch=x.
func mergePreviewHandler(projectPath string) http.HandlerFunc {
	ops := api.MergeOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		branchName := r.URL.Query().Get("branch")
		if branchName == "" {
			writeError(w, http.StatusBadRequest, "branch query parameter is required")
			return
		}
		result, err := ops.Preview(branchName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, mergeResultToMap(result))
	}
}

// mergeHandler handles POST /api/merge.
func mergeHandler(projectPath string) http.HandlerFunc {
	ops := api.MergeOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req struct {
			Branch string `json:"branch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Branch == "" {
			writeError(w, http.StatusBadRequest, "branch is required")
			return
		}
		result, err := ops.Merge(req.Branch)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, mergeResultToMap(result))
	}
}

// mergeAbortHandler handles POST /api/merge/abort.
func mergeAbortHandler(projectPath string) http.HandlerFunc {
	ops := api.MergeOps{ProjectRoot: projectPath}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if err := ops.Abort(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true, "success": true})
	}
}

// ─── status & storage handlers ───────────────────────────────────────────────

// statusHandler handles GET /api/status.
func statusHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		result, err := api.GetStatus(projectPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		files := make([]map[string]any, len(result.Files))
		for i, f := range result.Files {
			files[i] = map[string]any{
				"path":          f.Path,
				"type":          string(f.Type),
				"lines_added":   f.LinesAdded,
				"lines_removed": f.LinesRemoved,
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"branch_name":    result.BranchName,
			"snapshot_id":    result.SnapshotID,
			"snapshot_label": result.SnapshotLabel,
			"files":          files,
		})
	}
}

// storageHandler handles GET /api/storage.
func storageHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		result, err := api.GetStorage(projectPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
