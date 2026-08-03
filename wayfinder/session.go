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

// mapOverviewWindow is window 1 of a Map's session. It runs `pop map show`, so
// attaching to a Map opens on what the Map is deciding rather than on a bare
// shell, and every later window in the session is a grilling window.
const mapOverviewWindow = "map"

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

// MapSession is the tmux session one Map's windows live in.
type MapSession struct {
	Name string
	// Dir is the Trunk worktree the session was rooted at, empty when the session
	// already existed and no Trunk had to be resolved.
	Dir string
	// Created distinguishes the two halves of create-or-attach, which is the only
	// thing a report line has to say that the session name does not.
	Created bool
}

// GrillingWindow is one Decision ticket's window inside a Map's session.
type GrillingWindow struct {
	Session MapSession
	Window  string
	PaneID  string
	// Reused reports that a live grilling process was already in the window, so
	// it became a jump target rather than being sent the command again (ADR-0158).
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
	paneID, err := t.NewSessionWithWindow(name, dir, mapOverviewWindow)
	if err != nil {
		return nil, err
	}
	if err := t.StampWorkSession(name, string(ref.KindMap), mapID); err != nil {
		return nil, err
	}
	if err := t.SendKeys(paneID, mapOverviewCommand(d, mapID), "Enter"); err != nil {
		return nil, fmt.Errorf("run the map overview in %s: %w", name, err)
	}
	return &MapSession{Name: name, Dir: dir, Created: true}, nil
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

// OpenGrillingWindow lands command in ticket's window of the Map's session,
// creating the session when it has to. A window whose process is still alive is
// left alone and becomes a jump target (ADR-0158); an idle one (bare shell) is
// respawned. Focusing is the caller's call — the Work dashboard focuses on its
// way out, while `pop map next` focuses immediately.
func OpenGrillingWindow(d *Deps, mapID string, ticket Ticket, command string) (*GrillingWindow, error) {
	session, err := EnsureMapSession(d, mapID)
	if err != nil {
		return nil, err
	}
	if session.Dir == "" {
		return nil, ErrNoTrunk
	}
	t := d.tmux()
	window := grillingWindowName(ticket)
	paneID, created, err := tmux.EnsureWindow(t, session.Name, window, session.Dir)
	if err != nil {
		return nil, err
	}
	out := &GrillingWindow{Session: *session, Window: window, PaneID: paneID}
	if !created && liveGrillingPane(t, paneID) {
		out.Reused = true
		return out, nil
	}
	if err := t.SendKeys(paneID, command, "Enter"); err != nil {
		return nil, fmt.Errorf("send the grilling command: %w", err)
	}
	return out, nil
}

// FocusGrillingWindow puts the caller in a grilling window: switch-client from
// inside tmux, attach from outside.
func FocusGrillingWindow(d *Deps, win *GrillingWindow) error {
	t := d.tmux()
	if err := t.SelectPane(win.PaneID); err != nil {
		return err
	}
	if err := tmux.SwitchTarget(t, win.PaneID); err != nil {
		return fmt.Errorf("switch to %s:%s: %w", win.Session.Name, win.Window, err)
	}
	return nil
}

// GrillingInvocation is the attended agent command a grilling window runs: the
// configured interactive agent, opened on the wayfinding skill in work mode for
// one ticket. Both entry points into a grilling window — `pop map next` and the
// Work dashboard's map row — build it here, so a session started from either
// looks the same.
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

// grillingWindowName names a window after the ticket file's stem, so the window
// list of a Map session reads as the questions being decided in it.
func grillingWindowName(ticket Ticket) string {
	if stem := strings.TrimSuffix(strings.TrimSpace(ticket.File), ".md"); stem != "" {
		return stem
	}
	return ticket.ID
}

// mapOverviewCommand is what window 1 runs. It re-invokes this same binary
// rather than whatever `pop` resolves to on PATH, so a session opened by a
// development build shows that build's view of the Map.
func mapOverviewCommand(d *Deps, mapID string) string {
	return shellQuote(d.exe()) + " map show " + shellQuote(mapID)
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
