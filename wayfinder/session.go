package wayfinder

import (
	"errors"
	"fmt"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
)

// mapWindow is the one window of a Map's session. Every ticket being grilled is
// a tiled pane in it, tagged with the ticket id, so one window shows the whole
// frontier in flight (ADR-0182). There is no overview pane: `pop map status` is a
// verb the human types, and a session with nothing running holds a bare shell at
// the Trunk.
const mapWindow = "map"

// ErrNoTrunk refuses to root a Map's session anywhere but the Trunk worktree.
// Wayfinding writes nothing into the repository, so the session has no checkout
// of its own; the Trunk is where the code it is deciding about actually lives.
// The wording matches managed Task-set registration, which resolves the Trunk
// the same way and offers the same escape hatch.
var ErrNoTrunk = errors.New("no Trunk worktree configured; re-run with --trunk <path> to name one")

// MapIDFromSession is MapSessionName's inverse: the Map a session belongs to,
// or "" when the session is not one of pop's. It exists for the read paths that
// scan every live session and have only names to go on.
func MapIDFromSession(session string) string {
	if !strings.HasPrefix(session, mapSessionPrefix) {
		return ""
	}
	return strings.TrimPrefix(session, mapSessionPrefix)
}

// MapSession is the tmux session one Map's grilling panes live in.
type MapSession struct {
	Name string
	// Dir is the Trunk worktree the session was rooted at, empty when the session
	// already existed and no Trunk had to be resolved.
	Dir string
	// Created distinguishes the two halves of create-or-attach, which is the only
	// thing a report line has to say that the session name does not.
	Created bool
	// InitialPane is the pane tmux gave the session's `map` window on creation,
	// set only when this call created the session. Nothing has been sent to it, so
	// the first spawn claims it instead of splitting a second pane and leaving a
	// bare shell beside the agent.
	InitialPane string
}

// GrillingPane is one Decision ticket's pane inside a Map's session: tagged with
// the ticket id and titled after the ticket file's stem, tiled beside the rest of
// the frontier in the single `map` window.
type GrillingPane struct {
	Session MapSession
	Window  string
	PaneID  string
	// Title is the pane title, the ticket file's stem — the only thing in the
	// window that reads as which question the pane is deciding.
	Title string
	// Reused reports that a live grilling process was already in the pane, so it
	// became a jump target rather than being sent the command again (ADR-0158).
	Reused bool
}

// EnsureMapSession creates or reuses `pop-map-<map-id>`, stamped with the Work
// container it hosts. It is the auto-open half of the Map verbs' house rule:
// every write ensures the session and reports where it is, so a session is never
// something the human has to remember to open first.
//
// An existing session short-circuits before the Trunk is resolved, which is what
// keeps a repository with no configured Trunk usable once its session is up.
func EnsureMapSession(d *Deps, mapID string) (*MapSession, error) {
	name := MapSessionName(mapID)
	t := d.tmux()
	exists := t.HasSession(name)
	dir, err := d.trunk()
	if err != nil {
		if exists {
			// A live session is already rooted somewhere; the Trunk only has to
			// resolve when one has to be created. Reporting where a session is must
			// not depend on config a running session no longer needs.
			return &MapSession{Name: name}, nil
		}
		return nil, err
	}
	if exists {
		return &MapSession{Name: name, Dir: dir}, nil
	}
	paneID, err := t.NewSessionWithWindow(name, dir, mapWindow)
	if err != nil {
		return nil, err
	}
	if err := t.StampWorkSession(name, string(ref.KindMap), mapID); err != nil {
		return nil, err
	}
	return &MapSession{Name: name, Dir: dir, Created: true, InitialPane: paneID}, nil
}

// AttachMapSession is `pop map open`'s second half: ensure the session, then put
// the caller in it — switch-client from inside tmux, attach from outside.
func AttachMapSession(d *Deps, mapID string) (*MapSession, error) {
	session, err := EnsureMapSession(d, mapID)
	if err != nil {
		return nil, err
	}
	if err := tmux.SwitchTarget(d.tmux(), session.Name); err != nil {
		return nil, fmt.Errorf("switch to %s: %w", session.Name, err)
	}
	return session, nil
}

// openGrillingPane lands command in ticket's pane of the Map's `map` window,
// splitting and re-tiling one when the ticket has none. The ticket tag is the
// reuse key: a pane whose process is still alive is left alone and becomes a jump
// target (ADR-0158), while an idle one (bare shell) is respawned. Focusing is the
// caller's call — neither spawn verb moves the operator unless asked.
//
// session must be one EnsureMapSession just returned: a freshly created session's
// own first pane is adopted for this ticket rather than left as a bare shell
// beside the agent.
func openGrillingPane(d *Deps, session MapSession, ticket Ticket, command string) (*GrillingPane, error) {
	if session.Dir == "" {
		return nil, ErrNoTrunk
	}
	t := d.tmux()
	title := grillingPaneTitle(ticket)
	existing, err := t.FindTaggedPane(session.Name, mapWindow, tmux.TagTicket, ticket.ID)
	if err != nil {
		return nil, err
	}
	if existing != "" && liveGrillingPane(t, existing) {
		return &GrillingPane{Session: session, Window: mapWindow, PaneID: existing, Title: title, Reused: true}, nil
	}
	if existing == "" {
		if err := adoptIdleMapPane(t, session, ticket); err != nil {
			return nil, err
		}
	}
	paneID, err := tmux.EnsureTaggedPane(t, tmux.TagTicket, session.Name, mapWindow, session.Dir, ticket.ID, command)
	if err != nil {
		return nil, err
	}
	if err := t.SetPaneTitle(paneID, title); err != nil {
		return nil, fmt.Errorf("title the grilling pane: %w", err)
	}
	return &GrillingPane{Session: session, Window: mapWindow, PaneID: paneID, Title: title}, nil
}

// FocusMapSession puts the caller in a Map's `map` window: switch-client from
// inside tmux, attach from outside. It selects no particular pane — the window is
// the destination, and choosing one agent out of a tiled frontier for the human
// would be a guess.
func FocusMapSession(d *Deps, session MapSession) error {
	if err := tmux.SwitchToWindow(d.tmux(), session.Name, mapWindow); err != nil {
		return fmt.Errorf("switch to %s:%s: %w", session.Name, mapWindow, err)
	}
	return nil
}

// GrillingInvocation is the attended agent command a grilling pane runs: the
// configured interactive agent, opened on the wayfinding skill in work mode for
// one ticket. Every entry point — `pop map next`, `pop map fan-out`, and the Work
// dashboard's map row — builds it here, so a session started from any of them
// looks the same. It names exactly one ticket, which is what keeps the
// one-non-research-ticket-per-session rule intact across a fanned-out frontier.
func GrillingInvocation(cfg *config.Config, mapID, ticketID, dir string) (string, error) {
	skillsPrefix := config.DefaultSkillsPrefix
	if cfg != nil {
		skillsPrefix = cfg.ResolveSkillsPrefix()
	}
	prompt := WorkModeInvocation(skillsPrefix, mapID, ticketID)
	inv, err := tasks.ResolveAgentAssistanceInvocation(tasks.ResolveDefaultInteractiveAgentPreset(cfg), "", prompt, dir)
	if err != nil {
		return "", fmt.Errorf("resolve interactive agent: %w", err)
	}
	parts := []string{shellQuote(inv.Command.Name)}
	for _, arg := range inv.Command.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " "), nil
}

// adoptIdleMapPane gives this ticket the bare shell a Map session is born with,
// by tagging it before the shared primitive looks. A Map session is created by
// whichever write needed it first — `open`, or an auto-opening `register` — and its
// window comes with one pane; without this the first spawn would split a second and
// leave that shell sitting beside the agent forever.
//
// Adoption is deliberately narrow: exactly one pane in the window, carrying no
// ticket tag, running no process. Anything else is somebody's pane.
func adoptIdleMapPane(t tmux.Tmux, session MapSession, ticket Ticket) error {
	panes, err := t.WindowPanes(session.Name, mapWindow)
	if err != nil || len(panes) != 1 {
		return nil
	}
	tagged, err := t.PaneTagValue(panes[0], tmux.TagTicket)
	if err != nil || tagged != "" {
		return nil
	}
	if session.InitialPane != panes[0] && liveGrillingPane(t, panes[0]) {
		return nil
	}
	if err := t.TagPane(panes[0], tmux.TagTicket, ticket.ID); err != nil {
		return fmt.Errorf("claim the map window's idle pane: %w", err)
	}
	return nil
}

// liveGrillingPane reports whether a pane still holds a running process. A pane
// pop cannot read is treated as live, so an unreadable pane is never sent keys
// on top of whatever it is running.
func liveGrillingPane(t tmux.Tmux, paneID string) bool {
	info, err := t.PaneInfo(paneID)
	if err != nil {
		return true
	}
	return !tmux.IsBareShell(info.Command)
}

// grillingPaneTitle titles a pane with the ticket file's stem, so a wall of tiled
// panes reads as the questions being decided in it. The tag carries the identity;
// the title is what a human reads.
func grillingPaneTitle(ticket Ticket) string {
	if stem := strings.TrimSuffix(strings.TrimSpace(ticket.File), ".md"); stem != "" {
		return stem
	}
	return ticket.ID
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.ContainsAny(s, " \t\n'\"\\$`!&|;()<>*?[]#~") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
