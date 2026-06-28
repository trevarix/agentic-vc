// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	branchpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
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

	// API endpoints.
	mux.HandleFunc("/api/project", projectInfoHandler(projectPath))
	mux.HandleFunc("/api/snapshots", listSnapshotsHandler(projectPath))
	mux.HandleFunc("/api/snapshots/create", createSnapshotHandler(projectPath))
	mux.HandleFunc("/api/snapshots/", snapshotByIDHandler(projectPath))
	mux.HandleFunc("/api/diff", diffHandler(projectPath))
	mux.HandleFunc("/api/diff-current", diffCurrentHandler(projectPath))
	mux.HandleFunc("/api/restore", restoreHandler(projectPath))
	mux.HandleFunc("/api/restore-file", restoreFileHandler(projectPath))

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

// ─── handlers ───────────────────────────────────────────────────────────────

func projectInfoHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		branchName := branchpkg.GetActiveBranchName(projectPath)
		writeJSON(w, http.StatusOK, map[string]any{
			"path":           projectPath,
			"name":           filepath.Base(projectPath),
			"active_branch":  branchName,
		})
	}
}

func listSnapshotsHandler(projectPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		store, err := db.Open(projectPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer store.Close()

		var snapshots []*db.Snapshot
		branchID, branchErr := branchpkg.GetActiveBranchID(projectPath)
		if branchErr == nil {
			snapshots, err = store.ListSnapshotsByBranch(branchID)
		} else {
			snapshots, err = store.ListSnapshots()
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		out := make([]map[string]any, len(snapshots))
		for i, s := range snapshots {
			out[i] = map[string]any{
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
		writeJSON(w, http.StatusOK, out)
	}
}

type createSnapshotRequest struct {
	Label string `json:"label"`
	Agent string `json:"agent"`
	Notes string `json:"notes"`
}

func createSnapshotHandler(projectPath string) http.HandlerFunc {
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

		branchID, err := branchpkg.GetActiveBranchID(projectPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		branchName := branchpkg.GetActiveBranchName(projectPath)
		sourceDir := branchpkg.WorkspacePath(projectPath, branchName)

		snap, err := snapshot.Create(projectPath, req.Label, req.Agent, req.Notes, branchID, sourceDir)
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
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "snapshot id required")
			return
		}

		store, err := db.Open(projectPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer store.Close()

		switch r.Method {
		case http.MethodGet:
			snap, err := store.GetSnapshot(id)
			if err != nil {
				writeError(w, http.StatusNotFound, fmt.Sprintf("snapshot '%s' not found", id))
				return
			}
			files, err := store.GetSnapshotFiles(id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
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
			writeJSON(w, http.StatusOK, map[string]any{
				"id":            snap.ID,
				"label":         snap.Label,
				"timestamp":     snap.Timestamp,
				"agent_name":    snap.AgentName,
				"files_changed": snap.FileCount,
				"total_size":    snap.TotalSize,
				"notes":         snap.Notes,
				"branch_id":     snap.BranchID,
				"file_count":    len(files),
				"files":         fileList,
			})
		case http.MethodDelete:
			if _, err := store.GetSnapshot(id); err != nil {
				writeError(w, http.StatusNotFound, fmt.Sprintf("snapshot '%s' not found", id))
				return
			}
			if err := store.DeleteSnapshot(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
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

func formatDiffResult(result *diff.Result, from, to string) map[string]any {
	files := make([]map[string]any, len(result.Files))
	for i, f := range result.Files {
		files[i] = map[string]any{
			"path":          f.Path,
			"type":          string(f.Type),
			"old_hash":      f.OldHash,
			"new_hash":      f.NewHash,
			"lines_added":   f.LinesAdded,
			"lines_removed": f.LinesRemoved,
			"diff_preview":  f.DiffPreview,
		}
	}
	return map[string]any{
		"from_snapshot": from,
		"to_snapshot":   to,
		"files":         files,
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
		result, err := restore.RestoreFile(projectPath, req.ID, req.Path)
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

