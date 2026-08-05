package dashboardshell

import (
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks/drain"
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
	width  int
	height int
}

// RunFromQueue opens the dashboard on page A. It returns the bound checkout path
// chosen with Ctrl-g (empty otherwise), matching dashboard.RunDashboard.
func RunFromQueue(d *drain.Deps, cfg *config.Config) (string, error) {
	s, err := newShell(PageWork, d, cfg)
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
func RunFromRoutine(d *drain.Deps, cfg *config.Config) error {
	s, err := newShell(PageRoutines, d, cfg)
	if err != nil {
		return err
	}
	_, err = runShell(s)
	return err
}

func newShell(start Page, d *drain.Deps, cfg *config.Config) (Shell, error) {
	if d == nil {
		d = drain.DefaultDeps()
	}
	if cfg == nil {
		var err error
		cfg, err = d.LoadConfig(config.DefaultConfigPath())
		if err != nil {
			return Shell{}, err
		}
	}
	// The entry page is the one command the operator ran, so its build failure is
	// that command failing. The other page has no model at all yet.
	snap, err := dashboard.BuildPageSnapshot(d, cfg, start)
	if err != nil {
		return Shell{}, err
	}
	pages := map[Page]dashboard.QueueDashboard{start: dashboard.NewDashboardOn(d, cfg, snap, start)}
	return Shell{active: start, pages: pages, d: d, cfg: cfg}, nil
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
		var cmds []tea.Cmd
		for _, id := range []Page{PageWork, PageRoutines} {
			if _, built := s.pages[id]; built {
				cmds = append(cmds, s.updatePage(id, msg))
			}
		}
		return s, tea.Batch(cmds...)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
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
	return s, s.updatePage(s.active, msg)
}

func (s Shell) View() tea.View {
	return s.page(s.active).View()
}

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
	s.pages[s.active] = dashboard.OpenPage(s.d, s.cfg, s.active)
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
