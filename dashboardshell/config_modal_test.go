package dashboardshell

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// stubOverrideWriter is an override layer in memory: enough for the shell's half
// of the contract, which is about who owns the keyboard and what happens after a
// write, not about what the layer stores.
type stubOverrideWriter struct {
	copied []string
}

func (w *stubOverrideWriter) Store(string, string) (string, error) { return "", nil }

func (w *stubOverrideWriter) CopySource(key string) error {
	w.copied = append(w.copied, key)
	return nil
}

func (w *stubOverrideWriter) Remove(string) error { return nil }

func (w *stubOverrideWriter) Rows() ([]ui.ConfigDashboardRow, error) {
	return []ui.ConfigDashboardRow{{
		Key:  "work.attended.agents",
		Desc: "Ordered fallback agent list.",
		Preview: ui.ConfigDashboardPreview{
			ValueTOML:  `work.attended.agents = ["claude"]`,
			Provenance: "config.toml",
		},
	}}, nil
}

// shellWithConfigModal wires the shell's two modal seams to a fake layer and a
// scripted re-read, and returns the shell sized as a terminal.
func shellWithConfigModal(t *testing.T, start Page, writer ui.ConfigOverrideWriter, reload func() (*config.Config, error)) Shell {
	t.Helper()
	s, err := newShell(start, actionDeps(), &config.Config{}, "")
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	s.openConfig = func() *ui.ConfigDashboard {
		rows, _ := writer.Rows()
		return ui.NewConfigDashboard(rows, ui.ConfigDashboardOpts{Writer: writer})
	}
	s.reloadConfig = reload
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return updated.(Shell)
}

// actionDeps is testDeps with an action on page A's kind, so a host verb exists
// to prove inert while the modal is open.
func actionDeps() *drain.Deps {
	d := testDeps()
	d.Kinds = func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&actionKind{pageKind: &pageKind{
			id: ref.KindTaskSet, containers: setRows(),
			columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set",
		}}}
	}
	return d
}

// actionKind offers one action, which is all the `a` menu needs to open.
type actionKind struct{ *pageKind }

func (k *actionKind) Actions(work.Container) []work.Action {
	return []work.Action{{Key: "x", Label: "do the thing", Verb: work.Verb("test.thing")}}
}

func press(t *testing.T, s Shell, msg tea.KeyPressMsg) Shell {
	t.Helper()
	updated, _ := s.Update(msg)
	return updated.(Shell)
}

func altC() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt} }
func esc() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// The chord opens the component from either page, and closing it puts the human
// back on the page they left, cursor and all.
func TestConfigModalOpensFromEitherPageAndReturnsToIt(t *testing.T) {
	for _, start := range []Page{PageWork, PageRoutines} {
		s := shellWithConfigModal(t, start, &stubOverrideWriter{}, nil)
		s = press(t, s, tea.KeyPressMsg{Code: 'j', Text: "j"})
		cursor := s.PageDashboard(start).ListCursor()
		before := s.View().Content

		s = press(t, s, altC())
		if !s.ConfigModalOpen() {
			t.Fatalf("alt+c on page %v opened no modal", start)
		}
		if view := s.View().Content; !strings.Contains(view, "Config · what is in force here") {
			t.Fatalf("modal view on page %v:\n%s", start, view)
		}
		if s.ActivePage() != start {
			t.Fatalf("opening the modal moved the page to %v", s.ActivePage())
		}

		s = press(t, s, esc())
		if s.ConfigModalOpen() {
			t.Fatalf("esc left the modal open on page %v", start)
		}
		if s.ActivePage() != start {
			t.Fatalf("closing the modal landed on page %v, want %v", s.ActivePage(), start)
		}
		if got := s.PageDashboard(start).ListCursor(); got != cursor {
			t.Fatalf("cursor = %d after the modal, want the %d it was left on", got, cursor)
		}
		if s.View().Content != before {
			t.Fatalf("page %v changed across the modal:\n%s", start, s.View().Content)
		}
	}
}

// Decision 11's first half: while the modal is open the host's keys are fully
// suspended. The page toggle and a kind's action verb are the two the Work
// dashboard has, and neither reaches the page.
func TestConfigModalSuspendsEveryHostKey(t *testing.T) {
	// The control: with no modal open these keys are live, so their inertness
	// below is the modal's doing and not a page that ignores them anyway.
	// Each control gets its own shell: the pages map is shared by every copy of a
	// Shell value, so one control's keypress would otherwise land under the next.
	live := func() Shell { return shellWithConfigModal(t, PageWork, &stubOverrideWriter{}, nil) }
	if page := press(t, live(), tea.KeyPressMsg{Code: 'a', Text: "a"}).PageDashboard(PageWork); page.ViewToggleAllowed() {
		t.Fatal("`a` opens no action menu on this page — the fixture proves nothing")
	}
	if !press(t, live(), tea.KeyPressMsg{Code: '/', Text: "/"}).PageDashboard(PageWork).FilterActive() {
		t.Fatal("`/` engages no filter on this page — the fixture proves nothing")
	}
	if press(t, live(), tea.KeyPressMsg{Code: 'v', Text: "v"}).ActivePage() != PageRoutines {
		t.Fatal("`v` does not page this shell — the fixture proves nothing")
	}

	s := shellWithConfigModal(t, PageWork, &stubOverrideWriter{}, nil)
	cursor := s.PageDashboard(PageWork).ListCursor()
	s = press(t, s, altC())

	for _, key := range []tea.KeyPressMsg{
		{Code: 'v', Text: "v"}, // the shell's page toggle
		{Code: 'a', Text: "a"}, // the action menu, a kind's verbs
		{Code: 'j', Text: "j"}, // movement on the table
		{Code: '/', Text: "/"}, // the row filter
		{Code: 'g', Text: "g"}, // half of the jump-to-top pair
		{Code: 'g', Text: "g"},
	} {
		s = press(t, s, key)
	}

	if s.ActivePage() != PageWork {
		t.Fatalf("v paged the shell to %v while the modal was open", s.ActivePage())
	}
	if _, built := s.pages[PageRoutines]; built {
		t.Fatal("v built the other page while the modal was open")
	}
	if !s.ConfigModalOpen() {
		t.Fatal("the host keys closed the modal")
	}

	s = press(t, s, esc())
	page := s.PageDashboard(PageWork)
	if !page.ViewToggleAllowed() {
		t.Fatal("a key reached the page and opened an overlay on it")
	}
	if page.FilterActive() {
		t.Fatal("`/` reached the page and engaged its filter")
	}
	if got := page.ListCursor(); got != cursor {
		t.Fatalf("cursor = %d, want the %d it had before the modal opened", got, cursor)
	}
}

// The chord is the component's own, so a host key it collides with does not
// exist: `c` alone is not it, and neither is ctrl+c, which quits.
func TestConfigModalChordIsAltCAlone(t *testing.T) {
	s := shellWithConfigModal(t, PageWork, &stubOverrideWriter{}, nil)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'c', Text: "c"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		if press(t, s, key).ConfigModalOpen() {
			t.Fatalf("%v opened the Config modal", key)
		}
	}
}

// Decision 14: the shell loads config once, so after a write it re-reads and
// re-merges, and the pages report the new value. Nothing re-reads without a
// write.
func TestConfigModalWriteReReadsConfigForThePages(t *testing.T) {
	reloads := 0
	after := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Overridden Agent", Cmd: "codex --model gpt"},
		}},
	}}
	reload := func() (*config.Config, error) {
		reloads++
		return after, nil
	}

	s := shellWithConfigModal(t, PageWork, &stubOverrideWriter{}, reload)
	want := tasks.FormatAttendedAgentStatus(tasks.EffectiveAttendedEntry(after))
	if strings.Contains(s.View().Content, want) {
		t.Fatalf("the page already reports %q before any write", want)
	}

	// Open, close without writing: nothing to re-read.
	s = press(t, s, altC())
	s = press(t, s, esc())
	if reloads != 0 {
		t.Fatalf("re-read config %d times after a modal that wrote nothing", reloads)
	}

	// Open, copy the source down — a write — and close.
	s = press(t, s, altC())
	s = press(t, s, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	s = press(t, s, esc())
	if reloads != 1 {
		t.Fatalf("re-read config %d times after a write, want once", reloads)
	}
	if view := s.View().Content; !strings.Contains(view, want) {
		t.Fatalf("page does not report the re-read config %q:\n%s", want, view)
	}

	// The other page is built after the write and sees the same value: the shell's
	// own copy was replaced, not just the page that was showing.
	s = press(t, s, tea.KeyPressMsg{Code: 'v', Text: "v"})
	s = press(t, s, tea.KeyPressMsg{Code: 'v', Text: "v"})
	if view := s.View().Content; !strings.Contains(view, want) {
		t.Fatalf("page rebuilt after the write does not report it:\n%s", view)
	}
}

// A re-read that fails has nowhere to print — the component's whole contract is
// that this host writes nothing to stdout — so it lands in the page's own error
// chrome and the page keeps the config it had.
func TestConfigModalReReadFailureShowsOnThePage(t *testing.T) {
	reload := func() (*config.Config, error) { return nil, errors.New("config.toml is not readable") }
	s := shellWithConfigModal(t, PageWork, &stubOverrideWriter{}, reload)

	s = press(t, s, altC())
	s = press(t, s, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	s = press(t, s, esc())

	view := s.View().Content
	if !strings.Contains(view, "config.toml is not readable") {
		t.Fatalf("page does not report the failed re-read:\n%s", view)
	}
	want := tasks.FormatAttendedAgentStatus(tasks.EffectiveAttendedEntry(&config.Config{}))
	if !strings.Contains(view, want) {
		t.Fatalf("page dropped the config it was built with:\n%s", view)
	}
}
