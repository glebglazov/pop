package tasks

import (
	"strings"
	"testing"
)

func TestSelectTaskAutomaticPriorityOrder(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "blocked", Status: StatusBlocked, Priority: 100},
			{ID: "high", Status: StatusReady, Priority: 10},
			{ID: "low", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"high": {Stem: "high", Valid: true, Tasks: []Task{
				{ID: "02-b", File: "02-b.md", Type: "AFK", Status: "open"},
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"low": {Stem: "low", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	sel, err := SelectTask(refresh, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.TaskSetID != "high" || sel.TaskID != "02-b" {
		t.Fatalf("selection = %s/%s, want high/02-b", sel.TaskSetID, sel.TaskID)
	}
}

func TestSelectTaskExplicitRequiresTaskSet(t *testing.T) {
	_, err := SelectTask(&RefreshResult{}, "", "01-a")
	if err == nil {
		t.Fatal("expected error")
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitSetup {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectTaskExplicitRejectsDoneFailedHITLBlocked(t *testing.T) {
	base := &RefreshResult{
		Rows: []Row{{ID: "demo", Status: StatusReady, Priority: 0}},
		Manifests: map[string]*Manifest{
			"demo": {
				Stem:  "demo",
				Valid: true,
				Tasks: []Task{
					{ID: "01-done", File: "01-done.md", Type: "AFK", Status: "done"},
					{ID: "02-failed", File: "02-failed.md", Type: "AFK", Status: "failed"},
					{ID: "03-hitl", File: "03-hitl.md", Type: "HITL", Status: "open"},
					{ID: "04-blocked", File: "04-blocked.md", Type: "AFK", Status: "open", BlockedBy: []string{"02-failed"}},
				},
			},
		},
	}

	tests := []struct {
		task    string
		contain string
	}{
		{"01-done", "already done"},
		{"02-failed", "failed"},
		{"03-hitl", "HITL"},
		{"04-blocked", "blocked by"},
	}
	for _, tt := range tests {
		_, err := SelectTask(base, "demo", tt.task)
		if err == nil {
			t.Fatalf("task %s: expected error", tt.task)
		}
		if !strings.Contains(err.Error(), tt.contain) {
			t.Fatalf("task %s: err = %v", tt.task, err)
		}
		ee, ok := err.(*ExitError)
		if !ok || ee.Code != ExitNoRunnable {
			t.Fatalf("task %s: code = %v", tt.task, err)
		}
	}
}

func TestSelectTaskExplicitTaskSetOverride(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "auto", Status: StatusReady, Priority: 10},
			{ID: "target", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"auto": {Stem: "auto", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"target": {Stem: "target", Valid: true, Tasks: []Task{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	sel, err := SelectTask(refresh, "target", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.TaskSetID != "target" {
		t.Fatalf("prd = %q", sel.TaskSetID)
	}
}

func TestSelectTaskFailedTaskSetRejected(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{{ID: "broken", Status: StatusFailed, Priority: 10}},
		Manifests: map[string]*Manifest{
			"broken": {Stem: "broken", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "failed"},
			}},
		},
	}
	_, err := SelectTask(refresh, "broken", "01-a")
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectTaskSetExplicitHumanBlockedAttendable(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "ready", Status: StatusReady, Priority: 10},
			{ID: "target", Status: StatusAwaitingApproval, Priority: 0, BlockedReason: "HITL: 02-gate"},
		},
		Manifests: map[string]*Manifest{
			"ready": {Stem: "ready", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"target": {Stem: "target", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"},
				{ID: "02-gate", File: "02-gate.md", Type: "HITL", Status: "open"},
			}},
		},
	}

	// Explicit attendance works even when another Task set is Ready.
	got, fallback, err := SelectTaskSet(refresh, "target")
	if err != nil {
		t.Fatalf("explicit Human-blocked target: %v", err)
	}
	if fallback {
		t.Fatalf("explicit target must not be a HITL fallback")
	}
	if got != "target" {
		t.Fatalf("selected = %q, want target", got)
	}
}

func TestSelectTaskSetExplicitAFKBlockedRejected(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "target", Status: StatusBlocked, Priority: 0, BlockedReason: "blocked by 01-dep"},
		},
		Manifests: map[string]*Manifest{
			"target": {Stem: "target", Valid: true, Tasks: []Task{
				{ID: "01-dep", File: "01-dep.md", Type: "AFK", Status: "open", BlockedBy: []string{"02-other"}},
				{ID: "02-task", File: "02-task.md", Type: "AFK", Status: "open", BlockedBy: []string{"01-dep"}},
			}},
		},
	}

	_, _, err := SelectTaskSet(refresh, "target")
	if err == nil {
		t.Fatal("expected error for AFK-dependency-blocked target")
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitNoRunnable {
		t.Fatalf("code = %v", err)
	}
}

func TestSelectTaskSetAmbiguousHITLFallbackRejected(t *testing.T) {
	// Two Task sets are Human-blocked and none is Ready: a bare drain must refuse
	// to pick by priority and advise targeting one explicitly.
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "beta", Status: StatusAwaitingApproval, Priority: 100, BlockedReason: "HITL: 01-gate"},
			{ID: "alpha", Status: StatusAwaitingApproval, Priority: 0, BlockedReason: "HITL: 01-gate"},
		},
		Manifests: map[string]*Manifest{
			"beta": {Stem: "beta", Valid: true, Tasks: []Task{
				{ID: "01-gate", File: "01-gate.md", Type: "HITL", Status: "open"},
			}},
			"alpha": {Stem: "alpha", Valid: true, Tasks: []Task{
				{ID: "01-gate", File: "01-gate.md", Type: "HITL", Status: "open"},
			}},
		},
	}

	got, fallback, err := SelectTaskSet(refresh, "")
	if err == nil {
		t.Fatal("expected error for ambiguous HITL fallback")
	}
	if got != "" || fallback {
		t.Fatalf("ambiguous fallback must not select a set: got=%q fallback=%v", got, fallback)
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitNoRunnable {
		t.Fatalf("code = %v, want ExitNoRunnable", err)
	}
	msg := ee.Error()
	for _, want := range []string{"pop tasks implement <task-set>", "alpha", "beta"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	// Priority must not decide the gate: the higher-priority set is not auto-picked.
	if strings.Contains(msg, "alpha, beta") == false {
		t.Fatalf("attendable sets must be listed sorted, got %q", msg)
	}
}

func TestMarkRunTargetOverridesNextAndIsolated(t *testing.T) {
	// A running set reads RUN, not NEXT RUN: the run-next badge no longer applies
	// once the set is actually running.
	rows := []Row{
		{ID: "next", Priority: 5, Status: StatusReady, NextPick: true, PriorityShow: "5 NEXT"},
		{ID: "other", Priority: 1, Status: StatusReady, PriorityShow: "1"},
	}
	MarkRunTarget(rows, "next")
	if rows[0].PriorityShow != "5 RUN" {
		t.Fatalf("running next-pick = %q, want \"5 RUN\"", rows[0].PriorityShow)
	}

	rows = []Row{
		{ID: "next", Priority: 5, Status: StatusReady, NextPick: true, PriorityShow: "5 NEXT"},
		{ID: "other", Priority: 1, Status: StatusReady, PriorityShow: "1"},
	}
	MarkRunTarget(rows, "other")
	if rows[0].PriorityShow != "5 NEXT" || rows[1].PriorityShow != "1 RUN" {
		t.Fatalf("separate = %q, %q", rows[0].PriorityShow, rows[1].PriorityShow)
	}
}
