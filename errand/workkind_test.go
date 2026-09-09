package errand

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

func TestErrandFailureWorkKindRetriesAndDismissesWithoutCarryingOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	checkout := "/repo/feature"
	subject := store.CheckoutRemoval{Path: checkout, WorkingPath: "/repo", Branch: "feature", Force: true}
	if err := s.QueueCheckoutRemoval(subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "removal.md")
	secretBody := "many surviving paths that must not enter the store-backed row"
	if err := os.WriteFile(outputPath, []byte(secretBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if started, err := s.StartErrand(checkout, outputPath); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	if err := s.FailErrand(checkout); err != nil {
		t.Fatal(err)
	}

	k := NewKind(td)
	rows, err := k.Load()
	if err != nil || len(rows) != 1 {
		t.Fatalf("Load = %+v, %v", rows, err)
	}
	row := rows[0]
	if row.ID != failureID(checkout) || row.Status != "FAILED" || row.Worktree != checkout || row.ErrandSubject != checkout || row.Kind != ref.KindErrandFailure {
		t.Fatalf("failure row = %+v", row)
	}
	visible := work.StatusCellText(k.StatusCell(row)) + " " + row.DetailSections[0].Body
	if !strings.Contains(visible, checkout) || !strings.Contains(visible, outputPath) || strings.Contains(visible, secretBody) {
		t.Fatalf("visible failure = %q", visible)
	}
	if got := actionVerbs(k.Actions(row)); !slices.Equal(got, []work.Verb{VerbRetry, VerbDismiss}) {
		t.Fatalf("actions = %v", got)
	}
	artifacts, err := k.Artifacts(row)
	if err != nil || len(artifacts) != 1 || artifacts[0].Path != outputPath {
		t.Fatalf("artifacts = %+v, %v", artifacts, err)
	}

	if outcome, err := k.Perform(row, nil, VerbRetry); err != nil || outcome.Kind != work.OutcomeRefresh {
		t.Fatalf("retry = %+v, %v", outcome, err)
	}
	if rows, err := k.Load(); err != nil || len(rows) != 0 {
		t.Fatalf("rows while retry is queued = %+v, %v", rows, err)
	}
	stored, err := s.ListErrands()
	if err != nil || len(stored) != 1 || stored[0].State != store.ErrandQueued || stored[0].CheckoutRemoval != subject || stored[0].OutputPath != "" {
		t.Fatalf("retried record = %+v, %v", stored, err)
	}

	if started, err := s.StartErrand(checkout, "/new-output.md"); err != nil || !started {
		t.Fatalf("restart: %v, %v", started, err)
	}
	if err := s.FailErrand(checkout); err != nil {
		t.Fatal(err)
	}
	rows, err = k.Load()
	if err != nil || len(rows) != 1 {
		t.Fatalf("second failure = %+v, %v", rows, err)
	}
	if rows[0].ErrandOutputPath != "/new-output.md" || rows[0].ErrandOutputPath == outputPath {
		t.Fatalf("retry did not replace the output pointer: %+v", rows[0])
	}
	if outcome, err := k.Perform(rows[0], nil, VerbDismiss); err != nil || outcome.Kind != work.OutcomeRefresh {
		t.Fatalf("dismiss = %+v, %v", outcome, err)
	}
	stored, err = s.ListErrands()
	if err != nil || len(stored) != 0 {
		t.Fatalf("dismissed record = %+v, %v", stored, err)
	}
}

func TestSuccessfulErrandHasNoWorkContainer(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	subject := store.CheckoutRemoval{Path: "/repo/feature", WorkingPath: "/repo"}
	if err := s.QueueCheckoutRemoval(subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	if started, err := s.StartErrand(subject.Path, "/output.md"); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	if err := s.FinishErrand(subject.Path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rows, err := NewKind(td).Load(); err != nil || len(rows) != 0 {
		t.Fatalf("successful Errand rows = %+v, %v", rows, err)
	}
}

func TestFailureReportUsesTimestampedDocumentsWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	first, err := createFailureReport(dir, at, "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := createFailureReport(dir, at, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first.Name()) != "removal-20260909T123000Z.md" || filepath.Base(second.Name()) != "removal-20260909T123001Z.md" {
		t.Fatalf("documents = %q, %q", first.Name(), second.Name())
	}
	body, err := os.ReadFile(first.Name())
	if err != nil || string(body) != "first" {
		t.Fatalf("first document = %q, %v", body, err)
	}
}

func actionVerbs(actions []work.Action) []work.Verb {
	verbs := make([]work.Verb, 0, len(actions))
	for _, action := range actions {
		verbs = append(verbs, action.Verb)
	}
	return verbs
}

func TestKindID(t *testing.T) {
	if got := NewKind(nil).ID(); got != ref.KindErrandFailure {
		t.Fatalf("ID = %q", got)
	}
}
