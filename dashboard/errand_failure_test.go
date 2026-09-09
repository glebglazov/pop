package dashboard

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

func TestDashboardShowsErrandFailurePointerAndVerbs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "feature")
	outputPath := filepath.Join(t.TempDir(), "removal-20260909T120000Z.md")
	if err := s.QueueCheckoutRemoval(store.CheckoutRemoval{Path: checkout, WorkingPath: filepath.Dir(checkout)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if started, err := s.StartErrand(checkout, outputPath); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	if err := s.FailErrand(checkout); err != nil {
		t.Fatal(err)
	}

	d := &drain.Deps{Tasks: td, Project: project.DefaultDeps()}
	kinds := d.WorkKinds(&config.Config{})
	snap, err := work.BuildSnapshot(kinds)
	if err != nil {
		t.Fatal(err)
	}
	failures := snap.ContainersOfKind(ref.KindErrandFailure)
	if len(failures) != 1 {
		t.Fatalf("Errand failure rows = %+v", failures)
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: snap.Containers, Summary: snap.Summary})
	m.width, m.height = 500, 30
	view := m.View().Content
	for _, want := range []string{"remove checkout", checkout, "FAILED", outputPath} {
		if !strings.Contains(view, want) {
			t.Fatalf("dashboard does not show %q:\n%s", want, view)
		}
	}
	actions := newWorkKinds(kinds).actionsFor(failures[0])
	if len(actions) != 2 || actions[0].Label != "retry" || actions[1].Label != "dismiss" {
		t.Fatalf("Errand failure actions = %+v", actions)
	}
}
