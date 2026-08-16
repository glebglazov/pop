package dashboard

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// verbSpyKind is the Routine kind with its Perform recorded: the verbs it
// advertises are the real ones, so a test can press a key and read back which of
// them the surface asked the kind to run.
type verbSpyKind struct {
	*routine.Kind
	performed []work.Verb
}

func (k *verbSpyKind) Perform(_ work.Container, _ *work.Item, verb work.Verb) (work.Outcome, error) {
	k.performed = append(k.performed, verb)
	return work.Outcome{Kind: work.OutcomeMessage, Message: "fired"}, nil
}

func (k *verbSpyKind) Load() ([]work.Container, error) {
	return []work.Container{firedRoutineContainer()}, nil
}

func (k *verbSpyKind) ID() work.KindID { return ref.KindRoutine }

// TestFlatIDispatchesPerKind pins the flat list-level `I`: it runs whatever the
// cursored row's own kind advertises under that key — a Task set drains, a
// Routine fires — rather than being a Map-only shortcut that is dead everywhere
// else. The Map half is covered by TestDashboardMapRowISpawnsFocusesAndQuits and
// TestDashboardMapRowIEmptyFrontierMessage, which press the same flat key.
func TestFlatIDispatchesPerKind(t *testing.T) {
	t.Run("a routine row fires", func(t *testing.T) {
		spy := &verbSpyKind{Kind: routine.NewKind(nil)}
		kinds := func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{spy} }
		m := openPage(t, &drain.Deps{Kinds: kinds, RoutineKinds: kinds}, PageRoutines)

		pressKeys(t, m, "I")

		if fmt.Sprint(spy.performed) != fmt.Sprint([]work.Verb{routine.VerbFire}) {
			t.Fatalf("I on a routine row performed %v, want the kind's own fire verb", spy.performed)
		}
	})

	t.Run("a task-set row drains", func(t *testing.T) {
		repo, setID, _ := queuetest.SetupSpawnRepo(t, "flat-i", []queuetest.SpawnTask{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
		d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
		repoKey, err := drain.ResolveRepoKey(d, repo)
		if err != nil {
			t.Fatal(err)
		}
		row.RepoKey = repoKey
		row.CursorKey = "pop\x00" + setID
		t.Chdir(repo)

		m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
		updated, cmd := m.update(tea.KeyPressMsg{Code: 'I', Text: "I"})
		got := updated.(QueueDashboard)
		if cmd == nil {
			t.Fatal("I on a task-set row did not start a drain")
		}
		if _, ok := cmd().(dashboardDrainListMsg); !ok {
			t.Fatalf("I on an unbound set produced %T, want the drain target list", cmd())
		}
		if got.flash.Text() != dashboardHandoffPending {
			t.Fatalf("status = %q, want the handoff reassurance %q", got.flash.Text(), dashboardHandoffPending)
		}
	})
}
