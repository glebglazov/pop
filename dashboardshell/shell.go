package dashboardshell

import (
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
)

// The Work dashboard's entry layer: it opens the dashboard on one of its two
// pages and switches between them with `v`.
//
// The shell used to host two different TUIs as sibling views. Both pages are now
// the same Work dashboard model — one wired with the registered kinds, one with
// the Routine kind — so all that is left here is which page has the keyboard.
// Each page keeps its own cursor, filter and snapshot across a switch because
// each is its own model instance.

// Page selects which page of the Work dashboard the shell shows.
type Page = dashboard.Page

const (
	// PageWork is page A: Task sets and Maps.
	PageWork = dashboard.PageWork
	// PageRoutines is page B: Routines.
	PageRoutines = dashboard.PageRoutines
)

// Shell is the Work dashboard's two pages with one of them in focus.
//
// Only the page being looked at exists. A page is built the first time the
// operator switches to it, so opening the dashboard pays for one project scan
// instead of two (ADR-0189); the deps and config are kept for that later build.
type Shell struct {
	active Page
	pages  map[Page]dashboard.QueueDashboard
	d      *drain.Deps
	cfg    *config.Config
	// cfgPath is the hand-authored config this shell loaded, kept so the Config
	// modal edits and re-reads the same file the pages were built from.
	cfgPath string
	// pane is the launching pane's facts, read once at startup and kept for the
	// page the toggle builds later — the pin is per page, the tmux read is not.
	pane   work.PaneFacts
	width  int
	height int

	// configModal is the Config dashboard when it is open over the page in focus.
	// While it is set it owns the keyboard: see config_modal.go.
	configModal *ui.ConfigDashboard
	// openConfig builds that modal, and reloadConfig re-reads config after it
	// writes. Both are seams a test substitutes to drive the modal without a
	// config dir; nil is the real override layer and the real load.
	openConfig   func() *ui.ConfigDashboard
	reloadConfig func() (*config.Config, error)
}

// RunFromQueue opens the dashboard on page A. It returns the bound checkout path
// chosen with Ctrl-g (empty otherwise), matching dashboard.RunDashboard.
func RunFromQueue(d *drain.Deps, cfg *config.Config, cfgPath string) (string, error) {
	s, err := newShell(PageWork, d, cfg, cfgPath)
	if err != nil {
		return "", err
	}
	final, err := runShell(s)
	if err != nil {
		return "", err
	}
	return final.page(PageWork).OpenCheckout(), nil
}

// RunFromRoutine opens the same dashboard on page B — the whole of what
// `pop routine dashboard` is now.
func RunFromRoutine(d *drain.Deps, cfg *config.Config, cfgPath string) error {
	s, err := newShell(PageRoutines, d, cfg, cfgPath)
	if err != nil {
		return err
	}
	_, err = runShell(s)
	return err
}

// newShell builds the shell on its entry page. cfgPath is the file cfg was read
// from — the caller's `--config` where there is one — so the Config modal and
// the re-read that follows a write both work on the config the pages hold, not
// on whatever the default path resolves to.
func newShell(start Page, d *drain.Deps, cfg *config.Config, cfgPath string) (Shell, error) {
	if d == nil {
		d = drain.DefaultDeps()
	}
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	if cfg == nil {
		var err error
		cfg, err = d.LoadConfig(cfgPath)
		if err != nil {
			return Shell{}, err
		}
	}
	// The entry page is the one command the operator ran, so its build failure is
	// that command failing. The other page has no model at all yet.
	pane := launchPaneFacts(d)
	snap, err := dashboard.BuildPageSnapshot(d, cfg, start, pane)
	if err != nil {
		return Shell{}, err
	}
	pages := map[Page]dashboard.QueueDashboard{start: dashboard.NewDashboardOn(d, cfg, snap, start)}
	return Shell{active: start, pages: pages, d: d, cfg: cfg, cfgPath: cfgPath, pane: pane}, nil
}

// launchPaneFacts reads the launching pane's facts, whichever page the dashboard
// opened on. It is the session's one tmux round-trip for them: the shell keeps
// them, hands them to the entry build and to the page the toggle builds later,
// and each page model re-derives its own pins from the copy it holds.
//
// The dashboard still opens on the page it was asked for (ADR-0201 decision 5) —
// a pane attributed to a Routine does not drag the launch across the toggle. The
// pin is simply computed per page, and is waiting on page B when the human gets
// there.
func launchPaneFacts(d *drain.Deps) work.PaneFacts {
	return dashboard.LaunchPaneFacts(d.Tmux)
}

func runShell(s Shell) (Shell, error) {
	program := tea.NewProgram(s)
	final, err := program.Run()
	if err != nil {
		return Shell{}, err
	}
	if sh, ok := final.(Shell); ok {
		return sh, nil
	}
	return Shell{}, nil
}

func (s Shell) Init() tea.Cmd {
	return s.initActivePage()
}

func (s Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = msg.Width
		s.height = msg.Height
		if s.configModal != nil {
			s.configModal.SetSize(msg.Width, msg.Height)
		}
		var cmds []tea.Cmd
		for _, id := range []Page{PageWork, PageRoutines} {
			if _, built := s.pages[id]; built {
				cmds = append(cmds, s.updatePage(id, msg))
			}
		}
		return s, tea.Batch(cmds...)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// While the Config modal is open it owns the keyboard outright (ADR-0202
		// decision 11): no page toggle, no kind's action verb, nothing. It is the
		// only branch here that consumes a key without the page ever seeing it.
		if s.configModal != nil {
			s, cmd := s.updateConfigModal(msg)
			return s, cmd
		}
		if kpm, ok := keyMsg.(tea.KeyPressMsg); ok && ui.IsConfigDashboardKey(kpm) {
			return s.openConfigModal(), nil
		}
		if keyMsg.String() == "v" && s.activePageToggleAllowed() {
			s.active = s.page(s.active).OtherPage()
			sized := s.buildActivePage()
			return s, tea.Batch(sized, s.initActivePage())
		}
	}

	// Everything else goes to the page in focus. The page it was meant for is
	// stamped on the poll messages, so a reload in flight when the operator pressed
	// v is dropped by the page that receives it rather than landing in the wrong
	// table.
	//
	// Non-key messages reach both while the modal is open: the page keeps polling,
	// so it is current rather than stale when the modal closes, and the modal gets
	// the editor's own callback. Neither reads the other's messages.
	if s.configModal != nil {
		s, cmd := s.updateConfigModal(msg)
		return s, tea.Batch(cmd, s.updatePage(s.active, msg))
	}
	return s, s.updatePage(s.active, msg)
}

// View shows the page in focus, or the Config modal over it while that is open.
// The modal renders its whole own frame rather than a panel inside the page's:
// it is the same component `pop config dashboard` runs, so what a human learns
// in one place reads the same in the other.
func (s Shell) View() tea.View {
	if s.configModal != nil {
		return s.configModal.View()
	}
	return s.page(s.active).View()
}

// ConfigModalOpen reports whether the Config modal is showing, for tests.
func (s Shell) ConfigModalOpen() bool { return s.configModal != nil }

// updatePage hands msg to one page and keeps the model it returns.
func (s Shell) updatePage(id Page, msg tea.Msg) tea.Cmd {
	updated, cmd := s.page(id).Update(msg)
	if dash, ok := updated.(dashboard.QueueDashboard); ok {
		s.pages[id] = dash
	}
	return cmd
}

func (s Shell) page(id Page) dashboard.QueueDashboard {
	return s.pages[id]
}

func (s Shell) activePageToggleAllowed() bool {
	return s.page(s.active).ViewToggleAllowed()
}

// buildActivePage builds the page in focus if the operator has just switched to it
// for the first time. The build is synchronous, so the switch lands on rows
// instead of on an empty table waiting for the first poll, and it hands the fresh
// page the terminal size it missed while it did not exist.
func (s Shell) buildActivePage() tea.Cmd {
	if _, built := s.pages[s.active]; built {
		return nil
	}
	s.pages[s.active] = dashboard.OpenPage(s.d, s.cfg, s.active, s.pane)
	if s.width == 0 && s.height == 0 {
		return nil
	}
	return s.updatePage(s.active, tea.WindowSizeMsg{Width: s.width, Height: s.height})
}

func (s Shell) initActivePage() tea.Cmd {
	return s.page(s.active).Init()
}

// ActivePage exposes the page in focus for tests.
func (s Shell) ActivePage() Page {
	return s.active
}

// PageDashboard exposes one page's model for tests.
func (s Shell) PageDashboard(id Page) dashboard.QueueDashboard {
	return s.page(id)
}
