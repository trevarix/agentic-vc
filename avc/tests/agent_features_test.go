// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Plan 05 (agent-era features) tests: session attribution + change summaries
// + timeline (B3), avc watch (B1), avc bisect (B2), and cross-branch diff /
// stacked branches / merge train (B4).
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/bisect"
	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/fsck"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/timeline"
	"github.com/trevarix/agentic-vc/avc/internal/watch"
)

// waitFor polls cond every 100ms until it returns true or the timeout
// elapses. Used by the watch tests, which assert on a daemon's async work.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// enableRun flips the [run] enabled gate that bisect and --validate require.
func enableRun(t *testing.T, projectRoot string) {
	t.Helper()
	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Run.Enabled = true
	if err := config.Save(projectRoot, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

// mainSnapshots returns the snapshots on a branch, newest first.
func branchSnapshots(t *testing.T, projectRoot, branchID string) []*db.Snapshot {
	t.Helper()
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	snaps, err := store.ListSnapshotsByBranch(branchID)
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	return snaps
}

// ─── B3 · Session attribution + change summaries + timeline ─────────────────

func TestSessionAttribution_RoundTrip(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "a.txt", "one\n")

	res, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label:     "auto: before work",
		AgentName: "claude",
		BranchID:  mainBranchID,
		SessionID: "sess-42",
		Task:      "add auth endpoints",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions: %v", err)
	}
	if res.SessionID != "sess-42" || res.Task != "add auth endpoints" {
		t.Errorf("result session/task = %q/%q, want sess-42/add auth endpoints", res.SessionID, res.Task)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := store.GetSnapshot(res.ID)
	if err != nil {
		store.Close()
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.SessionID != "sess-42" || snap.Task != "add auth endpoints" {
		t.Errorf("stored session/task = %q/%q", snap.SessionID, snap.Task)
	}

	// The session filter finds it; a different session does not.
	matched, err := store.ListSnapshotsFiltered(db.SnapshotFilter{SessionID: "sess-42"})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	other, err := store.ListSnapshotsFiltered(db.SnapshotFilter{SessionID: "sess-other"})
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || len(other) != 0 {
		t.Errorf("session filter: matched=%d other=%d, want 1/0", len(matched), len(other))
	}
}

func TestSummarize_TableCases(t *testing.T) {
	cases := []struct {
		name  string
		files []*diff.FileDiff
		want  string
	}{
		{"empty", nil, "no changes"},
		{
			"single modified with counts",
			[]*diff.FileDiff{{Path: "auth.go", Type: diff.Modified, LinesAdded: 40, LinesRemoved: 12}},
			"1 file: modified auth.go (+40 -12)",
		},
		{
			"mixed types",
			[]*diff.FileDiff{
				{Path: "auth_test.go", Type: diff.Added},
				{Path: "auth.go", Type: diff.Modified, LinesAdded: 4, LinesRemoved: 1},
				{Path: "legacy.go", Type: diff.Deleted},
			},
			"3 files: added auth_test.go, modified auth.go (+4 -1), deleted legacy.go",
		},
		{
			"binary modified",
			[]*diff.FileDiff{{Path: "logo.png", Type: diff.Modified, Binary: true}},
			"1 file: modified logo.png (binary)",
		},
		{
			"truncates beyond three",
			[]*diff.FileDiff{
				{Path: "a", Type: diff.Added},
				{Path: "b", Type: diff.Added},
				{Path: "c", Type: diff.Added},
				{Path: "d", Type: diff.Added},
				{Path: "e", Type: diff.Added},
			},
			"5 files: added a, added b, added c, +2 more",
		},
	}
	for _, tc := range cases {
		if got := diff.Summarize(tc.files); got != tc.want {
			t.Errorf("%s: Summarize = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestChangeSummaries_PersistedAndComposable(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "a.txt", "one\ntwo\n")
	writeFile(t, projectRoot, "c.txt", "gone soon\n")
	first := createMainSnap(t, projectRoot, mainBranchID, "first")

	writeFile(t, projectRoot, "a.txt", "one\nTWO\n")   // modified
	writeFile(t, projectRoot, "b.txt", "new\n")        // added
	os.Remove(filepath.Join(projectRoot, "c.txt"))     // deleted
	second := createMainSnap(t, projectRoot, mainBranchID, "second")

	// Snapshot creation populated the diffs cache with per-file summaries.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.GetDiffCache(first.ID, second.ID)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("diff cache rows = %d, want 3", len(rows))
	}
	byPath := map[string]string{}
	for _, r := range rows {
		if r.ChangeSummary == "" {
			t.Errorf("row %s has empty change_summary", r.FilePath)
		}
		byPath[r.FilePath] = r.ChangeSummary
	}
	if byPath["a.txt"] != "modified a.txt (+1 -1)" {
		t.Errorf("a.txt summary = %q, want 'modified a.txt (+1 -1)'", byPath["a.txt"])
	}

	// The one-liner composed from cached rows matches the plan's shape.
	got := diff.SummarizeCached(rows)
	want := "3 files: added b.txt, modified a.txt (+1 -1), deleted c.txt"
	if got != want {
		t.Errorf("SummarizeCached = %q, want %q", got, want)
	}

	// The snapshot result carried the same summary.
	if second.Summary != want {
		t.Errorf("snapshot Summary = %q, want %q", second.Summary, want)
	}
}

func TestTimeline_GroupsSessionsAndInterleavesOps(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "f.txt", "v1\n")
	snapA, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label: "auto: before auth", AgentName: "claude", BranchID: mainBranchID,
		SessionID: "s1", Task: "auth work",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, projectRoot, "f.txt", "v2\n")
	snapB, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label: "auto: after auth", AgentName: "claude", BranchID: mainBranchID,
		SessionID: "s1", Task: "auth work",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, projectRoot, "g.txt", "other\n")
	if _, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label: "auto: docs pass", AgentName: "gpt", BranchID: mainBranchID,
		SessionID: "s2", Task: "write docs",
	}); err != nil {
		t.Fatal(err)
	}
	// A restore-style operation after the last snapshot.
	if err := recordRestoreOp(projectRoot, mainBranchID, snapB.ID, snapA.ID); err != nil {
		t.Fatalf("record op: %v", err)
	}

	result, err := timeline.Build(projectRoot, "main", "", 0)
	if err != nil {
		t.Fatalf("timeline.Build: %v", err)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (s1, s2)", len(result.Sessions))
	}
	s1, s2 := result.Sessions[0], result.Sessions[1]
	if s1.SessionID != "s1" || s2.SessionID != "s2" {
		t.Fatalf("session order = %q, %q; want s1, s2", s1.SessionID, s2.SessionID)
	}
	if s1.Task != "auth work" || len(s1.Agents) != 1 || s1.Agents[0] != "claude" {
		t.Errorf("s1 task/agents = %q/%v", s1.Task, s1.Agents)
	}
	if len(s1.Events) != 2 {
		t.Errorf("s1 events = %d, want 2 snapshots", len(s1.Events))
	}
	// The first snapshot has an "initial" summary; the second a real one.
	if !strings.HasPrefix(s1.Events[0].Summary, "initial snapshot") {
		t.Errorf("first summary = %q, want initial-snapshot form", s1.Events[0].Summary)
	}
	if want := "1 file: modified f.txt (+1 -1)"; s1.Events[1].Summary != want {
		t.Errorf("second summary = %q, want %q", s1.Events[1].Summary, want)
	}
	// The op attaches to the block in progress when it happened (s2).
	var opEvents int
	for _, e := range s2.Events {
		if e.Kind == timeline.KindOperation {
			opEvents++
			if e.OpKind != "restore" {
				t.Errorf("op kind = %q, want restore", e.OpKind)
			}
		}
	}
	if opEvents != 1 {
		t.Errorf("s2 operation events = %d, want 1", opEvents)
	}

	// Session filter: only s1 snapshots.
	filtered, err := timeline.Build(projectRoot, "main", "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Sessions) != 1 || filtered.Sessions[0].SessionID != "s1" {
		t.Fatalf("filtered sessions = %+v, want single s1 block", filtered.Sessions)
	}
	snapEvents := 0
	for _, e := range filtered.Sessions[0].Events {
		if e.Kind == timeline.KindSnapshot {
			snapEvents++
		}
	}
	if snapEvents != 2 {
		t.Errorf("filtered s1 snapshot events = %d, want 2", snapEvents)
	}
}

// ─── B1 · avc watch ──────────────────────────────────────────────────────────

// startWatcher runs the watch daemon in the background and returns a stop
// function that blocks until it exits.
func startWatcher(t *testing.T, projectRoot string, opts watch.Options) (stopWatch func()) {
	t.Helper()
	opts.Out = os.Stderr // daemon progress in test output when -v
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- watch.Run(projectRoot, opts, stop) }()
	// Give the daemon time to register its watches before the test writes.
	time.Sleep(500 * time.Millisecond)
	return func() {
		close(stop)
		if err := <-done; err != nil {
			t.Errorf("watch.Run returned error: %v", err)
		}
	}
}

func TestWatch_BurstOfWritesYieldsOneSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	stop := startWatcher(t, projectRoot, watch.Options{
		Debounce: 700 * time.Millisecond,
		Tick:     50 * time.Millisecond,
	})
	defer stop()

	for i := 0; i < 5; i++ {
		writeFile(t, projectRoot, fmt.Sprintf("src/f%d.txt", i), "content\n")
		time.Sleep(50 * time.Millisecond)
	}

	waitFor(t, 15*time.Second, "watch checkpoint", func() bool {
		return len(branchSnapshots(t, projectRoot, mainBranchID)) >= 1
	})
	// The debounce must coalesce the burst into exactly one snapshot — wait
	// past another debounce window and re-count.
	time.Sleep(1500 * time.Millisecond)
	snaps := branchSnapshots(t, projectRoot, mainBranchID)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %d, want exactly 1 for one burst", len(snaps))
	}
	if !strings.HasPrefix(snaps[0].Label, retention.WatchLabelPrefix) {
		t.Errorf("label = %q, want %q prefix", snaps[0].Label, retention.WatchLabelPrefix)
	}
	if snaps[0].AgentName != watch.AgentName {
		t.Errorf("agent = %q, want %q", snaps[0].AgentName, watch.AgentName)
	}
	if snaps[0].FileCount != 5 {
		t.Errorf("file count = %d, want all 5 burst files", snaps[0].FileCount)
	}
}

func TestWatch_IdleTreeYieldsZeroSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "existing.txt", "already here\n")

	stop := startWatcher(t, projectRoot, watch.Options{
		Debounce: 200 * time.Millisecond,
		Tick:     50 * time.Millisecond,
	})
	time.Sleep(2 * time.Second)
	stop()

	if n := len(branchSnapshots(t, projectRoot, mainBranchID)); n != 0 {
		t.Errorf("snapshots on idle tree = %d, want 0", n)
	}
}

func TestWatch_IgnoredChurnYieldsZeroSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, ".avcignore", "build/\n*.log\n")

	stop := startWatcher(t, projectRoot, watch.Options{
		Debounce: 200 * time.Millisecond,
		Tick:     50 * time.Millisecond,
	})

	for i := 0; i < 5; i++ {
		writeFile(t, projectRoot, fmt.Sprintf("build/out%d.o", i), "obj\n")
		writeFile(t, projectRoot, "noise.log", fmt.Sprintf("line %d\n", i))
		time.Sleep(60 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	stop()

	if n := len(branchSnapshots(t, projectRoot, mainBranchID)); n != 0 {
		t.Errorf("snapshots from ignored churn = %d, want 0", n)
	}
}

func TestWatch_WorkspaceEditSnapshotsToBranch(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "base\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/watched", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	stop := startWatcher(t, projectRoot, watch.Options{
		Debounce:          400 * time.Millisecond,
		Tick:              50 * time.Millisecond,
		IncludeWorkspaces: true,
	})
	defer stop()

	writeFile(t, ws, "app.go", "edited in workspace\n")

	waitFor(t, 15*time.Second, "branch checkpoint", func() bool {
		return len(branchSnapshots(t, projectRoot, b.ID)) >= 1
	})
	snaps := branchSnapshots(t, projectRoot, b.ID)
	if !strings.HasPrefix(snaps[0].Label, retention.WatchLabelPrefix) {
		t.Errorf("branch snapshot label = %q, want watch prefix", snaps[0].Label)
	}
	// Main must NOT have gained a snapshot from the workspace edit.
	if n := len(branchSnapshots(t, projectRoot, mainBranchID)); n != 1 {
		t.Errorf("main snapshots = %d, want 1 (the base only)", n)
	}
}

func TestWatch_SingleInstanceGuardAndStatus(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)

	stop := startWatcher(t, projectRoot, watch.Options{Tick: 50 * time.Millisecond})

	st, err := watch.Status(projectRoot)
	if err != nil || !st.Running {
		t.Fatalf("Status while running = %+v, %v; want Running", st, err)
	}
	if err := watch.Run(projectRoot, watch.Options{}, nil); err == nil ||
		!strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Run = %v, want already-running refusal", err)
	}

	stop()
	st, err = watch.Status(projectRoot)
	if err != nil || st.Running {
		t.Errorf("Status after stop = %+v, %v; want not running", st, err)
	}
}

func TestWatch_SoakWithConcurrentManualSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "seed.txt", "0\n")

	// Poll mode: deterministic tick-based evaluation racing manual snapshots.
	stop := startWatcher(t, projectRoot, watch.Options{
		Poll:        150 * time.Millisecond,
		MinInterval: 0,
	})

	for i := 1; i <= 15; i++ {
		writeFile(t, projectRoot, "seed.txt", fmt.Sprintf("%d\n", i))
		writeFile(t, projectRoot, fmt.Sprintf("gen/f%d.txt", i), "data\n")
		if _, err := snapshot.Create(projectRoot, fmt.Sprintf("manual-%d", i), "", "", mainBranchID, ""); err != nil {
			t.Fatalf("manual snapshot %d during soak: %v", i, err)
		}
		time.Sleep(80 * time.Millisecond)
	}
	stop()

	// The DB survived the contention and every stored object is intact.
	snaps := branchSnapshots(t, projectRoot, mainBranchID)
	if len(snaps) < 15 {
		t.Errorf("snapshots after soak = %d, want >= 15 manual ones", len(snaps))
	}
	res, err := fsck.Run(projectRoot, false)
	if err != nil {
		t.Fatalf("fsck after soak: %v", err)
	}
	if len(res.Corrupt) != 0 {
		t.Errorf("fsck found %d corrupt object(s) after soak", len(res.Corrupt))
	}
}

func TestRetention_PrunesWatchSnapshotsFirst(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	var deliberate []string
	for i := 0; i < 10; i++ {
		writeFile(t, projectRoot, "f.txt", fmt.Sprintf("v%d\n", i))
		label := retention.WatchLabelPrefix + fmt.Sprintf(" change %d", i)
		if i%5 == 0 {
			label = fmt.Sprintf("deliberate-%d", i)
		}
		snap, err := snapshot.Create(projectRoot, label, "", "", mainBranchID, "")
		if err != nil {
			t.Fatal(err)
		}
		if i%5 == 0 {
			deliberate = append(deliberate, snap.ID)
		}
	}

	// Cap watch snapshots at 3: of the 8 watch checkpoints, the 5 oldest go;
	// the 2 deliberate snapshots are untouchable by this rule.
	cfg := &config.RetentionConfig{MaxWatchSnapshotsPerBranch: 3}
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, os.Stderr); err != nil {
		t.Fatalf("Enforce: %v", err)
	}

	snaps := branchSnapshots(t, projectRoot, mainBranchID)
	watchCount := 0
	remaining := map[string]bool{}
	for _, s := range snaps {
		remaining[s.ID] = true
		if strings.HasPrefix(s.Label, retention.WatchLabelPrefix) {
			watchCount++
		}
	}
	if watchCount != 3 {
		t.Errorf("watch snapshots after prune = %d, want 3", watchCount)
	}
	for _, id := range deliberate {
		if !remaining[id] {
			t.Errorf("deliberate snapshot %s was pruned by the watch cap", id)
		}
	}
}

// ─── B2 · avc bisect ─────────────────────────────────────────────────────────

// bisectMarkerCmd fails (exit 1) when broken.txt exists and skips (exit 125)
// when skipme.txt exists, in the shell the sandbox uses per platform.
func bisectMarkerCmd() string {
	if runtime.GOOS == "windows" {
		// Parenthesized else-chain: without parens, cmd treats everything
		// after the first `if` as its body and never checks broken.txt.
		return "if exist skipme.txt (exit 125) else (if exist broken.txt (exit 1) else (exit 0))"
	}
	return "if [ -f skipme.txt ]; then exit 125; fi; test ! -f broken.txt"
}

// seedBisectHistory creates n snapshots on main; snapshots at or after
// brokenFrom (1-based) include broken.txt, and any index in skipAt includes
// skipme.txt. Returns the snapshot IDs in chronological order.
func seedBisectHistory(t *testing.T, projectRoot, mainBranchID string, n, brokenFrom int, skipAt map[int]bool) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		writeFile(t, projectRoot, "counter.txt", fmt.Sprintf("%d\n", i))
		if i == brokenFrom {
			writeFile(t, projectRoot, "broken.txt", "regression\n")
		}
		if skipAt[i] {
			writeFile(t, projectRoot, "skipme.txt", "unbuildable\n")
		} else {
			os.Remove(filepath.Join(projectRoot, "skipme.txt"))
		}
		snap := createMainSnap(t, projectRoot, mainBranchID, fmt.Sprintf("step-%d", i))
		ids = append(ids, snap.ID)
	}
	return ids
}

func TestBisect_FindsRegressionInLogNRuns(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	enableRun(t, projectRoot)

	const total, brokenFrom = 16, 10
	ids := seedBisectHistory(t, projectRoot, mainBranchID, total, brokenFrom, nil)

	result, err := bisect.Run(projectRoot, bisect.Options{
		GoodID:  ids[0],
		Command: bisectMarkerCmd(),
	})
	if err != nil {
		t.Fatalf("bisect.Run: %v", err)
	}
	if result.FirstBadID != ids[brokenFrom-1] {
		t.Errorf("first bad = %s, want %s (step-%d)", result.FirstBadID, ids[brokenFrom-1], brokenFrom)
	}
	if result.PredecessorID != ids[brokenFrom-2] {
		t.Errorf("predecessor = %s, want %s", result.PredecessorID, ids[brokenFrom-2])
	}
	if result.Steps > 5 {
		t.Errorf("steps = %d, want <= 5 (O(log n) over 14 candidates)", result.Steps)
	}
	if result.Ambiguous {
		t.Error("result flagged ambiguous with no skips")
	}
	if !strings.Contains(result.Summary, "broken.txt") {
		t.Errorf("summary %q should name broken.txt", result.Summary)
	}

	// The scratch workspace is gone.
	entries, _ := os.ReadDir(filepath.Join(projectRoot, ".avc", "workspaces"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bisect-") {
			t.Errorf("scratch workspace %s left behind", e.Name())
		}
	}
}

func TestBisect_SkipNarrowsAndFlagsAmbiguity(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	enableRun(t, projectRoot)

	// 8 snapshots, broken from 5, snapshot 4 unjudgeable.
	ids := seedBisectHistory(t, projectRoot, mainBranchID, 8, 5, map[int]bool{4: true})

	result, err := bisect.Run(projectRoot, bisect.Options{
		GoodID:  ids[0],
		Command: bisectMarkerCmd(),
	})
	if err != nil {
		t.Fatalf("bisect.Run: %v", err)
	}
	if result.FirstBadID != ids[4] {
		t.Errorf("first bad = %s, want %s (step-5)", result.FirstBadID, ids[4])
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != ids[3] {
		t.Errorf("skipped = %v, want [%s]", result.Skipped, ids[3])
	}
	if !result.Ambiguous {
		t.Error("expected ambiguous result: the skipped snapshot could be the true first bad")
	}
}

func TestBisect_RefusedWhenRunDisabled(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "f.txt", "x\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "only")

	_, err := bisect.Run(projectRoot, bisect.Options{GoodID: snap.ID, Command: "echo hi"})
	if err == nil || !strings.Contains(err.Error(), "[run] enabled") {
		t.Fatalf("err = %v, want [run] enabled refusal", err)
	}
}

// ─── B4 · Stacked branches, cross-branch diff, merge train ──────────────────

func TestCreateFromBranch_StacksOnParentHead(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "x.txt", "base\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	parent, err := branch.Create(projectRoot, "feat/parent", base.ID)
	if err != nil {
		t.Fatal(err)
	}
	ws := branch.WorkspacePath(projectRoot, parent.Name)
	writeFile(t, ws, "x.txt", "parent work\n")
	parentHead, err := snapshot.Create(projectRoot, "parent edit", "", "", parent.ID, ws)
	if err != nil {
		t.Fatal(err)
	}

	child, err := branch.CreateFromBranch(projectRoot, "feat/child", "feat/parent")
	if err != nil {
		t.Fatalf("CreateFromBranch: %v", err)
	}
	if child.BaseSnapshotID != parentHead.ID {
		t.Errorf("child base = %s, want parent HEAD %s", child.BaseSnapshotID, parentHead.ID)
	}
	if child.ParentBranchID != parent.ID {
		t.Errorf("child parent = %s, want %s", child.ParentBranchID, parent.ID)
	}
	// The child workspace starts from the parent's latest work.
	data, err := os.ReadFile(filepath.Join(branch.WorkspacePath(projectRoot, child.Name), "x.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "parent work\n" {
		t.Errorf("child workspace x.txt = %q, want parent's edit", data)
	}
}

// trainBranch creates a branch off base editing one file, snapshotted.
func trainBranch(t *testing.T, projectRoot, name, baseID, file, content string) *db.Branch {
	t.Helper()
	b, err := branch.Create(projectRoot, name, baseID)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, file, content)
	if _, err := snapshot.Create(projectRoot, name+" edit", "", "", b.ID, ws); err != nil {
		t.Fatalf("snapshot %s: %v", name, err)
	}
	return b
}

func TestMergeTrain_DisjointBranchesAllMerge(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "shared.txt", "base\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	trainBranch(t, projectRoot, "feat/a", base.ID, "a.txt", "from a\n")
	trainBranch(t, projectRoot, "feat/b", base.ID, "b.txt", "from b\n")
	trainBranch(t, projectRoot, "feat/c", base.ID, "c.txt", "from c\n")

	result, err := merge.Train(projectRoot, []string{"feat/a", "feat/b", "feat/c"}, "", false)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if result.Completed != 3 || result.StoppedAt != "" {
		t.Fatalf("completed=%d stopped=%q, want 3 merges and no stop", result.Completed, result.StoppedAt)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(projectRoot, name)); err != nil {
			t.Errorf("main is missing %s after the train", name)
		}
	}
}

func TestMergeTrain_StopsAtConflictKeepsEarlierMerges(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "shared.txt", "line-one\nline-two\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	// a and b rewrite the same line differently; c is disjoint.
	trainBranch(t, projectRoot, "feat/a", base.ID, "shared.txt", "line-A\nline-two\n")
	trainBranch(t, projectRoot, "feat/b", base.ID, "shared.txt", "line-B\nline-two\n")
	trainBranch(t, projectRoot, "feat/c", base.ID, "c.txt", "from c\n")

	result, err := merge.Train(projectRoot, []string{"feat/a", "feat/b", "feat/c"}, "", false)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if result.Completed != 1 || result.StoppedAt != "feat/b" {
		t.Fatalf("completed=%d stopped=%q, want 1 and feat/b", result.Completed, result.StoppedAt)
	}
	statuses := map[string]string{}
	for _, r := range result.Results {
		statuses[r.Branch] = r.Status
	}
	if statuses["feat/a"] != merge.TrainMerged ||
		statuses["feat/b"] != merge.TrainConflicts ||
		statuses["feat/c"] != merge.TrainSkipped {
		t.Errorf("statuses = %v", statuses)
	}
	// Main kept a's merge and carries no conflict markers.
	data, err := os.ReadFile(filepath.Join(projectRoot, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line-A\nline-two\n" {
		t.Errorf("shared.txt = %q, want feat/a's merge, unmarked", data)
	}
}

func TestMergeTrain_ValidateFailureRollsBackThatMerge(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	enableRun(t, projectRoot)
	writeFile(t, projectRoot, "base.txt", "base\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	b := trainBranch(t, projectRoot, "feat/broken", base.ID, "new.txt", "breaks the build\n")

	result, err := merge.Train(projectRoot, []string{"feat/broken"}, "exit 1", false)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if result.Completed != 0 || result.StoppedAt != "feat/broken" {
		t.Fatalf("completed=%d stopped=%q, want rollback stop", result.Completed, result.StoppedAt)
	}
	if result.Results[0].Status != merge.TrainValidationFailed {
		t.Fatalf("status = %q, want %q", result.Results[0].Status, merge.TrainValidationFailed)
	}
	// The merge was rolled back: main does not contain the branch's file...
	if _, err := os.Stat(filepath.Join(projectRoot, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt still on main — validation rollback did not restore the pre-merge state")
	}
	// ...and the branch is active again.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := store.GetBranchByID(b.ID)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != "active" {
		t.Errorf("branch status = %q, want active after rollback", fresh.Status)
	}
}

func TestMergeTrain_StackedChildMergesAfterParent(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "x.txt", "base\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	parent := trainBranch(t, projectRoot, "feat/parent", base.ID, "x.txt", "parent work\n")
	_ = parent
	child, err := branch.CreateFromBranch(projectRoot, "feat/child", "feat/parent")
	if err != nil {
		t.Fatal(err)
	}
	cws := branch.WorkspacePath(projectRoot, child.Name)
	writeFile(t, cws, "y.txt", "child work\n")
	if _, err := snapshot.Create(projectRoot, "child edit", "", "", child.ID, cws); err != nil {
		t.Fatal(err)
	}

	result, err := merge.Train(projectRoot, []string{"feat/parent", "feat/child"}, "", false)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if result.Completed != 2 || result.StoppedAt != "" {
		t.Fatalf("completed=%d stopped=%q, want both merged", result.Completed, result.StoppedAt)
	}
	for file, want := range map[string]string{"x.txt": "parent work\n", "y.txt": "child work\n"} {
		data, err := os.ReadFile(filepath.Join(projectRoot, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", file, data, want)
		}
	}
}
