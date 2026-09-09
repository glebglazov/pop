package store

import (
	"testing"
	"time"
)

func TestErrandRunningClaimSurvivesDuplicateRequest(t *testing.T) {
	s := openTestStore(t)
	subject := CheckoutRemoval{Path: "/checkout", WorkingPath: "/repo", Branch: "feature", Force: true}
	if err := s.QueueCheckoutRemoval(subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	if started, err := s.StartErrand(subject.Path, "/output.md"); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	if err := s.QueueCheckoutRemoval(CheckoutRemoval{Path: subject.Path, WorkingPath: "/different"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListErrands()
	if err != nil || len(rows) != 1 || rows[0].State != ErrandRunning || rows[0].CheckoutRemoval != subject || rows[0].OutputPath != "/output.md" {
		t.Fatalf("duplicate replaced running subject: %+v, %v", rows, err)
	}
	if started, err := s.StartErrand(subject.Path, "/other.md"); err != nil || started {
		t.Fatalf("second attempt: %v, %v", started, err)
	}
	if err := s.FinishErrand(subject.Path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishErrand(subject.Path, time.Now()); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListErrandCompletions()
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v, %v", events, err)
	}
}
