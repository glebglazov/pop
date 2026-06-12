package tasks

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTaskSetDetailFoldsFailedRetryCount(t *testing.T) {
	failedAfter := 3
	m := &Manifest{
		Valid: true,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "done"},
			{ID: "02-b", File: "02-b.md", Title: "Second", Type: "AFK", Status: "failed", FailedAfter: &failedAfter, BlockedBy: []string{"01-a"}},
		},
	}

	var buf bytes.Buffer
	RenderTaskSetDetail(&buf, "demo", nil, m)
	out := buf.String()

	if !strings.Contains(out, "failed(3)") {
		t.Fatalf("expected folded retry count failed(3):\n%s", out)
	}
	// Blocker is shown in the BLOCKED-BY column for the dependent task.
	if !strings.Contains(out, "01-a") || !strings.Contains(out, "02-b") {
		t.Fatalf("expected both task ids:\n%s", out)
	}
	// Manifest order preserved: 01-a before 02-b.
	if strings.Index(out, "01-a") > strings.Index(out, "02-b") {
		t.Fatalf("tasks out of manifest order:\n%s", out)
	}
}

func TestRenderTaskSetDetailMalformed(t *testing.T) {
	m := &Manifest{Valid: false, Errors: []string{"bad index.json"}}

	var buf bytes.Buffer
	RenderTaskSetDetail(&buf, "demo", &Row{ID: "demo", Status: StatusMalformed}, m)
	out := buf.String()

	if !strings.Contains(out, "malformed") || !strings.Contains(out, "bad index.json") {
		t.Fatalf("expected malformed summary with error:\n%s", out)
	}
	if strings.Contains(out, "STATUS") {
		t.Fatalf("malformed set should not print a task table:\n%s", out)
	}
}

func TestRenderTaskSetDetailMissing(t *testing.T) {
	var buf bytes.Buffer
	RenderTaskSetDetail(&buf, "gone", &Row{ID: "gone", Status: StatusMissing}, nil)
	out := buf.String()

	if !strings.Contains(out, "missing") {
		t.Fatalf("expected missing notice:\n%s", out)
	}
}
