package workload

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeriveStatusDeferredDoneAndSkipped(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "HITL", Status: "skipped"},
		},
	}
	if got := DeriveStatus(m); got != StatusDeferred {
		t.Fatalf("status = %q, want DEFERRED", got)
	}
}

func TestDeriveStatusNotDeferredWithOpenHITL(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "skipped"},
			{ID: "02-b", Type: "HITL", Status: "open"},
		},
	}
	if got := DeriveStatus(m); got == StatusDeferred {
		t.Fatalf("status = %q, want not DEFERRED (open HITL present)", got)
	}
}

func TestDeriveStatusNotDeferredWithOpenAFK(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "skipped"},
			{ID: "02-b", Type: "AFK", Status: "open"},
		},
	}
	if got := DeriveStatus(m); got != StatusReady {
		t.Fatalf("status = %q, want READY (open eligible AFK)", got)
	}
}

func TestDeriveStatusAllDoneIsDoneNotDeferred(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "done"},
		},
	}
	if got := DeriveStatus(m); got != StatusDone {
		t.Fatalf("status = %q, want DONE", got)
	}
}

// TestDeriveStatusPrecedence locks DONE → FAILED → READY → DEFERRED → BLOCKED.
func TestDeriveStatusPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		issues []Issue
		want   IssueSetStatus
	}{
		{
			name: "failed beats deferred",
			issues: []Issue{
				{ID: "01-a", Type: "AFK", Status: "failed"},
				{ID: "02-b", Type: "AFK", Status: "skipped"},
			},
			want: StatusFailed,
		},
		{
			name: "ready beats deferred",
			issues: []Issue{
				{ID: "01-a", Type: "AFK", Status: "open"},
				{ID: "02-b", Type: "AFK", Status: "skipped"},
			},
			want: StatusReady,
		},
		{
			name: "deferred beats blocked",
			issues: []Issue{
				{ID: "01-a", Type: "AFK", Status: "done"},
				{ID: "02-b", Type: "AFK", Status: "skipped"},
			},
			want: StatusDeferred,
		},
		{
			name: "blocked when open but not eligible and not all done-or-skipped",
			issues: []Issue{
				{ID: "01-a", Type: "AFK", Status: "open", BlockedBy: []string{"02-b"}},
				{ID: "02-b", Type: "HITL", Status: "open"},
			},
			want: StatusBlocked,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Valid: true, Issues: tc.issues}
			if got := DeriveStatus(m); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDeferredRowRendersSkippedCount(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "HITL", Status: "skipped"},
		},
	}
	row := buildIssueSetRow(RegisteredIssueSet{ID: "demo", Priority: 0}, m, 0)
	if row.Status != StatusDeferred {
		t.Fatalf("row status = %q, want DEFERRED", row.Status)
	}
	detail := rowDetail(row)
	if !strings.Contains(detail, "1 skipped") {
		t.Fatalf("detail = %q, want %q segment", detail, "1 skipped")
	}

	var buf bytes.Buffer
	Render(&buf, &RefreshResult{Rows: []Row{row}})
	out := buf.String()
	if !strings.Contains(out, "DEFERRED") || !strings.Contains(out, "1 skipped") {
		t.Fatalf("rendered table missing DEFERRED row with skipped count:\n%s", out)
	}
}

func TestSelectIssueSetSkipsDeferredLikeDone(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "deferred", Status: StatusDeferred, Priority: 100},
			{ID: "done", Status: StatusDone, Priority: 50},
			{ID: "ready", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"deferred": {Stem: "deferred", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"},
				{ID: "02-b", File: "02-b.md", Type: "AFK", Status: "skipped"},
			}},
			"done": {Stem: "done", Valid: true, Issues: []Issue{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "done"},
			}},
			"ready": {Stem: "ready", Valid: true, Issues: []Issue{
				{ID: "01-y", File: "01-y.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	id, err := SelectIssueSet(refresh, "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if id != "ready" {
		t.Fatalf("auto-selected %q, want ready (deferred and done passed over)", id)
	}
}
