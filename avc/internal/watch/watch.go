// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package watch implements `avc watch`: a foreground daemon that makes
// safety structural instead of behavioral. File changes in the project root
// (and every active branch workspace) are debounced and checkpointed as
// snapshots automatically, so every state the project passes through is
// recoverable whether or not an agent remembered to call avc_snapshot.
package watch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// AgentName is recorded on every watch snapshot.
const AgentName = "avc-watch"

const (
	pidFileName = "watch.pid"
	// heartbeatInterval is how often the running daemon refreshes the pid
	// file's mtime; staleAfter is how old the mtime may be before the pid
	// file is considered left over from a crashed daemon. mtime heartbeats
	// are the liveness signal because PID liveness probes are not portable
	// to Windows.
	heartbeatInterval = 30 * time.Second
	staleAfter        = 90 * time.Second
	// maxLabelLen caps the checkpoint label length (the change description
	// is truncated to fit).
	maxLabelLen = 96
	// refreshEvery is how often the target list (branch workspaces) is
	// re-discovered so branches created while watching get picked up.
	refreshEvery = 30 * time.Second
	// eventQueueSize buffers change events between the fsnotify pump and the
	// main loop. Overflow degrades to "mark everything dirty", never loss.
	eventQueueSize = 4096
)

// Options configures a Run. Zero durations fall back to the config-file
// values and then the built-in defaults.
type Options struct {
	Debounce          time.Duration
	MinInterval       time.Duration
	IncludeWorkspaces bool
	Poll              time.Duration // > 0: poll with this interval instead of using fsnotify
	Out               io.Writer     // progress log; nil = io.Discard

	// Tick is the granularity of the debounce scanner. 0 = 1s. Tests use a
	// small value so short debounce windows resolve promptly.
	Tick time.Duration
}

// OptionsFromConfig resolves the [watch] config section into Options.
func OptionsFromConfig(cfg *config.Config) Options {
	opts := Options{
		Debounce:          config.DefaultWatchDebounceSeconds * time.Second,
		MinInterval:       config.DefaultWatchMinIntervalSeconds * time.Second,
		IncludeWorkspaces: true,
	}
	if cfg == nil {
		return opts
	}
	if cfg.Watch.DebounceSeconds > 0 {
		opts.Debounce = time.Duration(cfg.Watch.DebounceSeconds) * time.Second
	}
	if cfg.Watch.MinIntervalSeconds > 0 {
		opts.MinInterval = time.Duration(cfg.Watch.MinIntervalSeconds) * time.Second
	}
	if cfg.Watch.IncludeWorkspaces != nil {
		opts.IncludeWorkspaces = *cfg.Watch.IncludeWorkspaces
	}
	return opts
}

// target is one directory tree the daemon checkpoints: the project root
// (main) or a branch workspace.
type target struct {
	branchID   string
	branchName string
	sourceDir  string // projectRoot for main; the workspace path otherwise

	dirty     bool      // change events seen since the last evaluation
	lastEvent time.Time // most recent change event
	lastSnap  time.Time // most recent checkpoint taken by this daemon
}

// StatusResult reports whether a watcher is running for a project.
type StatusResult struct {
	Running   bool  `json:"running"`
	PID       int   `json:"pid,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"` // Unix time of the last heartbeat
}

// Status inspects the pid file. Running means the file exists and its
// heartbeat is fresh (see staleAfter).
func Status(projectRoot string) (*StatusResult, error) {
	path := filepath.Join(projectRoot, ".avc", pidFileName)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &StatusResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if time.Since(info.ModTime()) > staleAfter {
		return &StatusResult{Running: false, PID: pid, UpdatedAt: info.ModTime().Unix()}, nil
	}
	return &StatusResult{Running: true, PID: pid, UpdatedAt: info.ModTime().Unix()}, nil
}

// Run starts the daemon and blocks until stop is closed or a fatal error
// occurs. Only one daemon may run per project (pid-file guard).
func Run(projectRoot string, opts Options, stop <-chan struct{}) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}
	if opts.Debounce <= 0 {
		opts.Debounce = config.DefaultWatchDebounceSeconds * time.Second
	}
	if opts.MinInterval < 0 {
		opts.MinInterval = 0
	}

	release, err := acquirePidFile(projectRoot)
	if err != nil {
		return err
	}
	defer release()

	targets, err := discoverTargets(projectRoot, opts.IncludeWorkspaces)
	if err != nil {
		return err
	}
	logf(opts.Out, "watching %d tree(s); debounce %s, min interval %s",
		len(targets), opts.Debounce, opts.MinInterval)

	if opts.Poll > 0 {
		return runPoll(projectRoot, opts, targets, stop)
	}
	return runNotify(projectRoot, opts, targets, stop)
}

// runPoll evaluates every target on a fixed interval — the fallback for
// filesystems where change notification is unreliable (network mounts).
// Idle trees cost a stat-only walk per tick thanks to the stat cache.
func runPoll(projectRoot string, opts Options, targets []*target, stop <-chan struct{}) error {
	ticker := time.NewTicker(opts.Poll)
	defer ticker.Stop()
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-stop:
			return nil
		case <-heartbeat.C:
			touchPidFile(projectRoot)
		case <-ticker.C:
			if fresh, err := discoverTargets(projectRoot, opts.IncludeWorkspaces); err == nil {
				targets = mergeTargets(targets, fresh)
			}
			now := time.Now()
			for _, t := range targets {
				if now.Sub(t.lastSnap) < opts.MinInterval {
					continue
				}
				evaluate(projectRoot, t, opts.Out)
			}
		}
	}
}

// runNotify is the fsnotify-based mode: change events mark a target dirty,
// and a 1s scanner checkpoints each dirty target once its debounce quiet
// period has elapsed (and no sooner than MinInterval after its previous
// checkpoint).
func runNotify(projectRoot string, opts Options, targets []*target, stop <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("start file watcher: %w", err)
	}
	defer watcher.Close()

	for _, t := range targets {
		if err := addTree(watcher, projectRoot, t); err != nil {
			return err
		}
	}
	workspacesDir := filepath.Join(projectRoot, ".avc", "workspaces")
	if opts.IncludeWorkspaces {
		// Watch the workspaces parent (non-recursively) so branches created
		// while the daemon runs are picked up immediately.
		_ = os.MkdirAll(workspacesDir, 0755)
		if err := watcher.Add(workspacesDir); err != nil {
			return fmt.Errorf("watch %s: %w", workspacesDir, err)
		}
	}

	// Pump watcher.Events into a buffered queue on a dedicated goroutine.
	// The main loop calls watcher.Add (addTree) — if it consumed
	// watcher.Events directly, the fsnotify backend could block delivering
	// an event while Add waits for that same backend: deadlock. The pump
	// keeps the backend drained no matter what the main loop is doing; on
	// queue overflow it marks every target dirty rather than lose a change.
	events := make(chan fsnotify.Event, eventQueueSize)
	var overflowed atomic.Bool
	go func() {
		defer close(events)
		for ev := range watcher.Events {
			select {
			case events <- ev:
			default:
				overflowed.Store(true)
			}
		}
	}()

	ticker := time.NewTicker(opts.Tick)
	defer ticker.Stop()
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	refresh := time.NewTicker(refreshEvery)
	defer refresh.Stop()

	for {
		select {
		case <-stop:
			return nil

		case <-heartbeat.C:
			touchPidFile(projectRoot)

		case <-refresh.C:
			if fresh, err := discoverTargets(projectRoot, opts.IncludeWorkspaces); err == nil {
				targets = mergeTargets(targets, fresh)
				for _, t := range targets {
					_ = addTree(watcher, projectRoot, t) // idempotent; new dirs only
				}
			}

		case err := <-watcher.Errors:
			if err != nil {
				logf(opts.Out, "watch error: %v", err)
			}

		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("file watcher closed unexpectedly")
			}
			if ev.Op == fsnotify.Chmod {
				continue // metadata noise
			}
			// A new directory needs its own watch (fsnotify is not recursive).
			if ev.Op.Has(fsnotify.Create) {
				if info, statErr := os.Stat(ev.Name); statErr == nil && info.IsDir() {
					if fresh, err := discoverTargets(projectRoot, opts.IncludeWorkspaces); err == nil {
						targets = mergeTargets(targets, fresh)
					}
					for _, t := range targets {
						if within(t.sourceDir, ev.Name) {
							_ = addTree(watcher, projectRoot, t)
						}
					}
				}
			}
			if t := classify(projectRoot, workspacesDir, targets, ev.Name, opts.IncludeWorkspaces); t != nil {
				t.dirty = true
				t.lastEvent = time.Now()
			}

		case <-ticker.C:
			now := time.Now()
			if overflowed.Swap(false) {
				// Events were dropped — assume everything may have changed.
				// evaluate dedupes against HEAD, so a false positive costs
				// one cheap stat-cache pass, never a wrong snapshot.
				for _, t := range targets {
					t.dirty = true
					t.lastEvent = now
				}
			}
			for _, t := range targets {
				if !t.dirty || now.Sub(t.lastEvent) < opts.Debounce {
					continue
				}
				if now.Sub(t.lastSnap) < opts.MinInterval {
					continue // stays dirty; retried on a later tick
				}
				t.dirty = false // events during evaluation re-mark it
				evaluate(projectRoot, t, opts.Out)
			}
		}
	}
}

// evaluate checkpoints t if its tree differs from the branch HEAD. A tree
// identical to HEAD produces no snapshot — idle projects generate zero
// checkpoints no matter how often this runs.
func evaluate(projectRoot string, t *target, out io.Writer) {
	changed, desc, err := treeChanged(projectRoot, t)
	if err != nil {
		logf(out, "%s: check failed: %v", t.branchName, err)
		return
	}
	if !changed {
		return
	}
	label := retention.WatchLabelPrefix + " " + desc
	if len([]rune(label)) > maxLabelLen {
		label = string([]rune(label)[:maxLabelLen-1]) + "…"
	}
	res, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label:     label,
		AgentName: AgentName,
		BranchID:  t.branchID,
		SourceDir: sourceDirArg(projectRoot, t),
	})
	if err != nil {
		logf(out, "%s: checkpoint failed: %v", t.branchName, err)
		return
	}
	t.lastSnap = time.Now()
	logf(out, "%s: checkpoint %s — %s", t.branchName, res.ID, desc)
}

// sourceDirArg converts a target to the SourceDir convention used by
// snapshot.Create ("" means the project root).
func sourceDirArg(projectRoot string, t *target) string {
	if t.sourceDir == projectRoot {
		return ""
	}
	return t.sourceDir
}

// treeChanged reports whether t's tree differs from its branch HEAD, with a
// short description of what changed. The stat cache makes an unchanged tree
// a stat-only pass. Files over the snapshot size cap are skipped exactly as
// snapshot.Create skips them — otherwise an oversized file would read as a
// perpetual "change" and loop the daemon forever.
func treeChanged(projectRoot string, t *target) (bool, string, error) {
	headFiles := map[string]string{}
	store, err := db.Open(projectRoot)
	if err != nil {
		return false, "", err
	}
	head, headErr := store.GetHeadSnapshot(t.branchID)
	if headErr == nil {
		files, err := store.GetSnapshotFiles(head.ID)
		if err != nil {
			store.Close()
			return false, "", err
		}
		for _, f := range files {
			headFiles[f.RelativePath] = f.FileHash
		}
	}
	store.Close()

	cfg, _ := config.Load(projectRoot)
	maxFileSizeMB := config.DefaultMaxFileSizeMB
	if cfg != nil && cfg.Snapshot.MaxFileSizeMB > 0 {
		maxFileSizeMB = cfg.Snapshot.MaxFileSizeMB
	}
	maxBytes := int64(maxFileSizeMB) * 1024 * 1024

	ignore, err := ignoreRulesFor(projectRoot, t.sourceDir)
	if err != nil {
		return false, "", err
	}
	paths, err := fileutil.WalkProject(t.sourceDir, ignore)
	if err != nil {
		return false, "", err
	}
	cache := loadCache(projectRoot, t.sourceDir)

	current := make(map[string]string, len(paths))
	for _, abs := range paths {
		rel, err := filepath.Rel(t.sourceDir, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(abs)
		if err != nil || info.Size() > maxBytes {
			continue
		}
		if h, hit := cache.Lookup(rel, info); hit {
			current[rel] = h
			continue
		}
		h, err := fileutil.HashFile(abs)
		if err != nil {
			continue
		}
		current[rel] = h
	}

	var changed []string
	for p, h := range current {
		if headFiles[p] != h {
			changed = append(changed, p)
		}
	}
	for p := range headFiles {
		if _, ok := current[p]; !ok {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return false, "", nil
	}
	if headErr != nil {
		return true, fmt.Sprintf("initial state (%d files)", len(current)), nil
	}
	return true, describeChanges(changed), nil
}

// describeChanges renders a compact "3 files: a, b, +1 more" description
// for checkpoint labels.
func describeChanges(paths []string) string {
	const maxListed = 2
	noun := "files"
	if len(paths) == 1 {
		noun = "file"
	}
	listed := paths
	extra := ""
	if len(paths) > maxListed {
		listed = paths[:maxListed]
		extra = fmt.Sprintf(", +%d more", len(paths)-maxListed)
	}
	return fmt.Sprintf("%d %s: %s%s", len(paths), noun, strings.Join(listed, ", "), extra)
}

// ignoreRulesFor mirrors snapshot.Create's rule resolution: a workspace's
// own .avcignore wins over the project root's when present.
func ignoreRulesFor(projectRoot, sourceDir string) (*fileutil.IgnoreRules, error) {
	if sourceDir != projectRoot {
		wsIgnore := filepath.Join(sourceDir, ".avcignore")
		if _, err := os.Stat(wsIgnore); err == nil {
			return fileutil.LoadIgnoreRulesFrom(wsIgnore)
		}
	}
	return fileutil.LoadIgnoreRules(projectRoot)
}

// loadCache loads the stat cache matching snapshot.Create's choice for the
// same source directory, so cache hits line up with what snapshots wrote.
func loadCache(projectRoot, sourceDir string) *statcache.Cache {
	if sourceDir == projectRoot {
		c, _ := statcache.Load(projectRoot)
		return c
	}
	workspacesBase := filepath.Join(projectRoot, ".avc", "workspaces")
	if rel, err := filepath.Rel(workspacesBase, sourceDir); err == nil && !strings.HasPrefix(rel, "..") {
		c, _ := statcache.LoadFromPath(statcache.WorkspaceCachePath(projectRoot, rel))
		return c
	}
	return statcache.Empty()
}

// discoverTargets returns the project root (main) plus every active branch
// workspace directory that exists on disk.
func discoverTargets(projectRoot string, includeWorkspaces bool) ([]*target, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}
	main, err := store.EnsureMainBranch(proj.ID)
	if err != nil {
		return nil, err
	}
	targets := []*target{{branchID: main.ID, branchName: "main", sourceDir: projectRoot}}
	if !includeWorkspaces {
		return targets, nil
	}
	branches, err := store.ListBranchesByStatus(proj.ID, "active")
	if err != nil {
		return nil, err
	}
	for _, b := range branches {
		if b.Name == "main" {
			continue
		}
		ws := branch.WorkspacePath(projectRoot, b.Name)
		if ws == "" {
			continue
		}
		if info, err := os.Stat(ws); err != nil || !info.IsDir() {
			continue // workspace not materialized
		}
		targets = append(targets, &target{branchID: b.ID, branchName: b.Name, sourceDir: ws})
	}
	return targets, nil
}

// mergeTargets keeps the runtime state (dirty/lastSnap) of existing targets
// while adopting additions and removals from a fresh discovery.
func mergeTargets(old, fresh []*target) []*target {
	byID := make(map[string]*target, len(old))
	for _, t := range old {
		byID[t.branchID] = t
	}
	out := make([]*target, 0, len(fresh))
	for _, f := range fresh {
		if existing, ok := byID[f.branchID]; ok {
			out = append(out, existing)
		} else {
			out = append(out, f)
		}
	}
	return out
}

// classify maps an event path to the target it belongs to, or nil when the
// path is out of scope (inside .avc but not a workspace, ignored, or in a
// VCS metadata directory).
func classify(projectRoot, workspacesDir string, targets []*target, path string, includeWorkspaces bool) *target {
	avcDir := filepath.Join(projectRoot, ".avc")
	if within(avcDir, path) {
		if !includeWorkspaces || !within(workspacesDir, path) {
			return nil
		}
		for _, t := range targets {
			if t.sourceDir != projectRoot && within(t.sourceDir, path) {
				if pathIgnored(projectRoot, t, path) {
					return nil
				}
				return t
			}
		}
		return nil
	}
	for _, t := range targets {
		if t.sourceDir == projectRoot {
			if pathIgnored(projectRoot, t, path) {
				return nil
			}
			return t
		}
	}
	return nil
}

// pathIgnored reports whether path is excluded from t's snapshots — by the
// always-skipped VCS/metadata directories or the .avcignore rules. Ignored
// churn (build output, logs) must not reset debounce timers or mark targets
// dirty, or a busy build directory would produce endless checkpoints.
func pathIgnored(projectRoot string, t *target, path string) bool {
	rel, err := filepath.Rel(t.sourceDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return true
	}
	rel = filepath.ToSlash(rel)
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".avc", ".git", ".hg", ".svn", ".bzr":
			return true
		}
	}
	ignore, err := ignoreRulesFor(projectRoot, t.sourceDir)
	if err != nil {
		return false
	}
	// The path may be a file or a directory; check both interpretations
	// plus every ancestor directory (a file inside an ignored directory is
	// ignored even though its own name matches nothing).
	if ignore.Matches(rel) || ignore.MatchesDir(rel) {
		return true
	}
	for dir := filepath.ToSlash(filepath.Dir(rel)); dir != "." && dir != "/"; dir = filepath.ToSlash(filepath.Dir(dir)) {
		if ignore.MatchesDir(dir) {
			return true
		}
	}
	return false
}

// addTree registers watches for t.sourceDir and every non-ignored
// subdirectory. Adding an already-watched directory is a no-op, so this is
// safe to call repeatedly as new directories appear.
func addTree(watcher *fsnotify.Watcher, projectRoot string, t *target) error {
	ignore, err := ignoreRulesFor(projectRoot, t.sourceDir)
	if err != nil {
		return err
	}
	return filepath.WalkDir(t.sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // a directory vanishing mid-walk is not fatal
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".avc", ".git", ".hg", ".svn", ".bzr":
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(t.sourceDir, path)
		if relErr == nil && rel != "." && ignore.MatchesDir(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		if addErr := watcher.Add(path); addErr != nil {
			return fmt.Errorf("watch %s: %w", path, addErr)
		}
		return nil
	})
}

// within reports whether path is inside (or equal to) dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ─── pid file ────────────────────────────────────────────────────────────────

// acquirePidFile enforces the single-instance guard. A fresh pid file from
// another daemon refuses the start; a stale one (no heartbeat for
// staleAfter) is replaced.
func acquirePidFile(projectRoot string) (func(), error) {
	path := filepath.Join(projectRoot, ".avc", pidFileName)
	if st, err := Status(projectRoot); err == nil && st.Running {
		return nil, fmt.Errorf("a watcher is already running for this project (pid %d) — stop it first, or wait %s if it crashed", st.PID, staleAfter)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("write pid file: %w", err)
	}
	return func() { os.Remove(path) }, nil
}

// touchPidFile refreshes the heartbeat mtime. Best-effort.
func touchPidFile(projectRoot string) {
	now := time.Now()
	_ = os.Chtimes(filepath.Join(projectRoot, ".avc", pidFileName), now, now)
}

func logf(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "[avc-watch] %s %s\n",
		time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}
