// Package errand queues human-requested acts beside the Work seam.
package errand

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func QueueCheckoutRemoval(td *tasks.Deps, subject store.CheckoutRemoval) error {
	if !filepath.IsAbs(subject.Path) || !filepath.IsAbs(subject.WorkingPath) {
		return fmt.Errorf("checkout removal requires absolute paths")
	}
	subject.Path = filepath.Clean(subject.Path)
	subject.WorkingPath = filepath.Clean(subject.WorkingPath)
	if subject.Path == subject.WorkingPath || subject.Path == string(filepath.Separator) {
		return fmt.Errorf("checkout removal requires a separate Git working path")
	}
	s, _, err := td.Store(true)
	if err != nil {
		return err
	}
	if err := s.QueueCheckoutRemoval(subject, td.Now()); err != nil {
		return err
	}
	wake(td)
	return nil
}

// Recover runs only after the daemon holds its exclusive lock. A running row
// already points at an interruption report written before removal could start.
func Recover(td *tasks.Deps) error {
	s, ok, err := td.Store(false)
	if err != nil || !ok {
		return err
	}
	rows, err := s.ListErrands()
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.State == store.ErrandRunning {
			f, err := os.OpenFile(e.OutputPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
			if err != nil {
				return err
			}
			_, writeErr := io.WriteString(f, "\nInterrupted: the daemon stopped before this errand finished. Retry is manual.\n")
			closeErr := f.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				return err
			}
			if err := s.FailErrand(e.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

// Tick attempts each queued subject once. Failed subjects remain untouched until
// another explicit request queues them; Work backoff does not apply here.
func Tick(td *tasks.Deps, pd *project.Deps, out io.Writer) error {
	s, ok, err := td.Store(false)
	if err != nil || !ok {
		return err
	}
	rows, err := s.ListErrands()
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.State != store.ErrandQueued {
			continue
		}
		if err := run(td, pd, s, e, out); err != nil {
			return err
		}
	}
	return nil
}

func run(td *tasks.Deps, pd *project.Deps, s *store.Store, e store.Errand, out io.Writer) error {
	dir := filepath.Join(filepath.Dir(tasks.DrainStorePathWith(td)), "errands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	attemptedAt := td.Now()
	interrupted := renderFailureReport(e.Path, attemptedAt, "Interrupted: the daemon stopped before this errand finished. Retry is manual.\n")
	report, err := createFailureReport(dir, attemptedAt, interrupted)
	if err != nil {
		return err
	}
	defer report.Close()
	started, err := s.StartErrand(e.Path, report.Name())
	if err != nil || !started {
		_ = os.Remove(report.Name())
		return err
	}

	var output bytes.Buffer
	removalDeps := *pd
	removalDeps.Warn = func(message string) {
		fmt.Fprintln(&output, message)
		fmt.Fprintln(out, message)
	}
	removeErr := project.RemoveCheckout(&removalDeps, e.WorkingPath, e.Path)
	hist, historyErr := history.LoadWith(&history.Deps{FS: td.FS, Tasks: td})
	if historyErr == nil {
		historyErr = hist.Remove(e.Path)
	}
	if historyErr != nil {
		fmt.Fprintf(&output, "History: %v\n", historyErr)
	}
	if removeErr == nil && e.Branch != "" {
		flag := "-d"
		if e.Force {
			flag = "-D"
		}
		if _, err := td.Git.CommandInDir(e.WorkingPath, "branch", flag, e.Branch); err != nil {
			removeErr = fmt.Errorf("delete branch %s: %w", e.Branch, err)
		}
	}
	if removeErr != nil {
		fmt.Fprintln(&output, removeErr)
	} else {
		fmt.Fprintln(&output, "Removed checkout.")
	}
	if err := os.WriteFile(report.Name(), []byte(renderFailureReport(e.Path, attemptedAt, output.String())), 0o600); err != nil {
		return errors.Join(err, s.FailErrand(e.Path))
	}
	if removeErr != nil {
		fmt.Fprintf(out, "errand: removal failed for %s; output: %s\n", e.Path, report.Name())
		return s.FailErrand(e.Path)
	}
	if err := s.FinishErrand(e.Path, td.Now()); err != nil {
		return err
	}
	_ = os.Remove(report.Name())
	fmt.Fprintf(out, "errand: removed checkout %s\n", e.Path)
	return nil
}

const failureReportTimeLayout = "20060102T150405Z"

// createFailureReport reserves one timestamped document without overwriting an
// earlier attempt. A collision advances the document instant, as the Task pass
// reports do.
func createFailureReport(dir string, at time.Time, body string) (*os.File, error) {
	at = at.UTC().Truncate(time.Second)
	for {
		path := filepath.Join(dir, "removal-"+at.Format(failureReportTimeLayout)+".md")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			at = at.Add(time.Second)
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(f, body); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return nil, err
		}
		return f, nil
	}
}

func renderFailureReport(path string, at time.Time, body string) string {
	return fmt.Sprintf("# Errand failure — Checkout removal\n\n- Attempted: %s\n- Subject: %s\n\n%s",
		at.UTC().Format(time.RFC3339), path, body)
}
