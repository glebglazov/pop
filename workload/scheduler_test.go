package workload

import (
	"strings"
	"testing"
)

func TestSelectIssueAutomaticPriorityOrder(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "blocked", Status: StatusBlocked, Priority: 100},
			{ID: "high", Status: StatusReady, Priority: 10},
			{ID: "low", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"high": {Stem: "high", Valid: true, Issues: []Issue{
				{ID: "02-b", File: "02-b.md", Type: "AFK", Status: "open"},
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"low": {Stem: "low", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	sel, err := SelectIssue(refresh, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.IssueSetID != "high" || sel.IssueID != "02-b" {
		t.Fatalf("selection = %s/%s, want high/02-b", sel.IssueSetID, sel.IssueID)
	}
}

func TestSelectIssueExplicitRequiresIssueSet(t *testing.T) {
	_, err := SelectIssue(&RefreshResult{}, "", "01-a")
	if err == nil {
		t.Fatal("expected error")
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitSetup {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectIssueExplicitRejectsDoneFailedHITLBlocked(t *testing.T) {
	base := &RefreshResult{
		Rows: []Row{{ID: "demo", Status: StatusReady, Priority: 0}},
		Manifests: map[string]*Manifest{
			"demo": {
				Stem:  "demo",
				Valid: true,
				Issues: []Issue{
					{ID: "01-done", File: "01-done.md", Type: "AFK", Status: "done"},
					{ID: "02-failed", File: "02-failed.md", Type: "AFK", Status: "failed"},
					{ID: "03-hitl", File: "03-hitl.md", Type: "HITL", Status: "open"},
					{ID: "04-blocked", File: "04-blocked.md", Type: "AFK", Status: "open", BlockedBy: []string{"02-failed"}},
				},
			},
		},
	}

	tests := []struct {
		issue   string
		contain string
	}{
		{"01-done", "already done"},
		{"02-failed", "failed"},
		{"03-hitl", "HITL"},
		{"04-blocked", "blocked by"},
	}
	for _, tt := range tests {
		_, err := SelectIssue(base, "demo", tt.issue)
		if err == nil {
			t.Fatalf("issue %s: expected error", tt.issue)
		}
		if !strings.Contains(err.Error(), tt.contain) {
			t.Fatalf("issue %s: err = %v", tt.issue, err)
		}
		ee, ok := err.(*ExitError)
		if !ok || ee.Code != ExitNoRunnable {
			t.Fatalf("issue %s: code = %v", tt.issue, err)
		}
	}
}

func TestSelectIssueExplicitIssueSetOverride(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "auto", Status: StatusReady, Priority: 10},
			{ID: "target", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"auto": {Stem: "auto", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"target": {Stem: "target", Valid: true, Issues: []Issue{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	sel, err := SelectIssue(refresh, "target", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.IssueSetID != "target" {
		t.Fatalf("prd = %q", sel.IssueSetID)
	}
}

func TestSelectIssueFailedIssueSetRejected(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{{ID: "broken", Status: StatusFailed, Priority: 10}},
		Manifests: map[string]*Manifest{
			"broken": {Stem: "broken", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "failed"},
			}},
		},
	}
	_, err := SelectIssue(refresh, "broken", "01-a")
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestMarkRunTargetCombinedAndSeparate(t *testing.T) {
	rows := []Row{
		{ID: "auto", Priority: 5, Status: StatusReady, AutoPick: true, PriorityShow: "5 AUTO"},
		{ID: "other", Priority: 1, Status: StatusReady, PriorityShow: "1"},
	}
	MarkRunTarget(rows, "auto")
	if rows[0].PriorityShow != "5 AUTO RUN" {
		t.Fatalf("combined = %q", rows[0].PriorityShow)
	}

	rows = []Row{
		{ID: "auto", Priority: 5, Status: StatusReady, AutoPick: true, PriorityShow: "5 AUTO"},
		{ID: "other", Priority: 1, Status: StatusReady, PriorityShow: "1"},
	}
	MarkRunTarget(rows, "other")
	if rows[0].PriorityShow != "5 AUTO" || rows[1].PriorityShow != "1 RUN" {
		t.Fatalf("separate = %q, %q", rows[0].PriorityShow, rows[1].PriorityShow)
	}
}
