package workload

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshAutoRegistration(t *testing.T) {
	root := t.TempDir()
	// An Issue set without any PRD is discovered and registered.
	setupManifest(t, root, "new-feature", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "new-feature" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Workloads[result.DefinitionPath]
	if entry == nil || len(entry.IssueSets) != 1 || entry.IssueSets[0].Priority != 0 {
		t.Fatalf("state = %#v", entry)
	}

	// Persisted registration uses the issue_sets key and stores only id + priority.
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"issue_sets\"") {
		t.Fatalf("state file missing issue_sets key:\n%s", raw)
	}
	if strings.Contains(string(raw), "\"prds\"") || strings.Contains(string(raw), "\"title\"") {
		t.Fatalf("state file has stale PRD fields:\n%s", raw)
	}
}

func TestRefreshEmptyWorkloadNoStateFile(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("rows = %d", len(result.Rows))
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("expected no state file for empty workload")
	}
}

func TestRefreshTableSections(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "done-prd", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	setupManifest(t, root, "active-high", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "active-low", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Workloads: map[string]*WorkloadEntry{canon: {IssueSets: []RegisteredIssueSet{
			{ID: "gone-prd", Priority: 5},
			{ID: "done-prd", Priority: 0},
			{ID: "active-high", Priority: 10},
			{ID: "active-low", Priority: 0},
		}}},
		path: statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}

	wantOrder := []string{"gone-prd", "done-prd", "active-high", "active-low"}
	if len(result.Rows) != len(wantOrder) {
		t.Fatalf("rows = %d, want %d: %#v", len(result.Rows), len(wantOrder), result.Rows)
	}
	for i, id := range wantOrder {
		if result.Rows[i].ID != id {
			t.Fatalf("row[%d] = %q, want %q", i, result.Rows[i].ID, id)
		}
	}
	if result.Rows[0].Status != StatusMissing {
		t.Fatalf("first status = %q", result.Rows[0].Status)
	}
	if result.Rows[1].Status != StatusDone {
		t.Fatalf("done status = %q", result.Rows[1].Status)
	}
	if result.Rows[2].Priority != 10 || result.Rows[3].Priority != 0 {
		t.Fatalf("active priorities wrong: %d, %d", result.Rows[2].Priority, result.Rows[3].Priority)
	}
}

func TestIssueSetWithoutPRDIsRegisteredAndReady(t *testing.T) {
	root := t.TempDir()
	// No thoughts/prds at all — a valid Issue set must still register and be Ready.
	setupManifest(t, root, "standalone", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "standalone" || result.Rows[0].Status != StatusReady {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "standalone" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}
}

func TestStatusDerivation(t *testing.T) {
	root := t.TempDir()

	setupManifest(t, root, "ready", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "blocked-hitl", []Issue{
		{ID: "01-hitl", File: "01-hitl.md", Title: "H", Type: "HITL", Status: "open"},
	})
	setupManifest(t, root, "failed", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})

	statePath := filepath.Join(root, "state.json")
	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}

	statusByID := map[string]IssueSetStatus{}
	for _, row := range result.Rows {
		statusByID[row.ID] = row.Status
	}

	if statusByID["ready"] != StatusReady {
		t.Fatalf("ready = %q", statusByID["ready"])
	}
	if statusByID["blocked-hitl"] != StatusBlocked {
		t.Fatalf("blocked = %q", statusByID["blocked-hitl"])
	}
	if statusByID["failed"] != StatusFailed {
		t.Fatalf("failed = %q", statusByID["failed"])
	}
}

func TestRenderDiagnostics(t *testing.T) {
	root := t.TempDir()
	issueDir := filepath.Join(root, "thoughts/issues/bad")
	writeIssueMD(t, issueDir, "01-a.md", "## Acceptance criteria\n\n- [ ] a\n")
	writeManifest(t, issueDir, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "in_progress"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "MALFORMED") {
		t.Fatalf("missing MALFORMED in output:\n%s", out)
	}
	if !strings.Contains(out, "Malformed diagnostics:") {
		t.Fatalf("missing diagnostics in output:\n%s", out)
	}
	if !strings.Contains(out, "in_progress") {
		t.Fatalf("missing detail in output:\n%s", out)
	}
}

func TestFailedRowResetHints(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "failed-prd", []Issue{
		{ID: "01-broken", File: "repair-broken.md", Title: "B", Type: "AFK", Status: "failed"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != StatusFailed {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if len(result.Rows[0].ResetHints) != 1 || result.Rows[0].ResetHints[0] != "pop workload reset-issue thoughts/issues/failed-prd/repair-broken.md" {
		t.Fatalf("reset hints = %v", result.Rows[0].ResetHints)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	if !strings.Contains(buf.String(), "reset: pop workload reset-issue") {
		t.Fatalf("output missing reset hint: %s", buf.String())
	}
}

func TestBlockedReasonInTable(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "blocked", []Issue{
		{ID: "01-hitl", File: "01-hitl.md", Title: "H", Type: "HITL", Status: "open"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0].BlockedReason != "HITL: 01-hitl" {
		t.Fatalf("blocked reason = %q", result.Rows[0].BlockedReason)
	}
}

func TestUnreadableDiscoveryDoesNotMutateState(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	setupManifest(t, root, "a", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	if _, err := RefreshWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	issueDir := filepath.Join(root, "thoughts/issues")
	if err := os.Chmod(issueDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(issueDir, 0o755) })

	_, err := RefreshWith(DefaultDeps(), root, statePath)
	if err == nil {
		t.Fatal("expected error")
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(root)
	if len(state.Workloads[canon].IssueSets) != 1 {
		t.Fatalf("state mutated: %#v", state.Workloads[canon])
	}
}

func setupManifest(t *testing.T, root, stem string, issues []Issue) {
	t.Helper()
	issueDir := filepath.Join(root, "thoughts/issues", stem)
	for _, issue := range issues {
		writeIssueMD(t, issueDir, issue.File, "## Acceptance criteria\n\n- [ ] ok\n")
	}
	writeManifest(t, issueDir, issues)
}

func writeManifest(t *testing.T, issueDir string, issues []Issue) {
	t.Helper()
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"issues": issues}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
