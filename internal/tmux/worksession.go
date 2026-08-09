package tmux

import (
	"fmt"
	"strings"
)

// A Work session is a tmux session pop opened for one Work container: a Map's
// grilling panes, later a Task set's or a Routine's. The container it belongs
// to is stamped on the session as pop-owned user options rather than recorded in
// pop.db, because the fact describes a *live* session — tying its lifetime to
// tmux's means there is never a stale row to reconcile. As with every other
// @pop_* option, the keys are constructed only here.
const (
	optWorkKind = "@pop_work_kind"
	optWorkID   = "@pop_work_id"
)

// WorkSession is a live tmux session stamped with the Work container it hosts.
// Kind is the Work kind's wire name (map | task-set | routine) — this module
// does not know the enum, only that the two options travel together.
type WorkSession struct {
	Session string
	Kind    string
	ID      string
	// Dir is tmux's own start directory for the session (`#{session_path}`) — the
	// directory it was created in, which for a Map session is the repository's
	// Trunk worktree. It rides along on the same list-sessions format string as
	// the stamp, so a consumer that has to place a Work session in a project tree
	// gets the fact for no extra process spawn and no filesystem I/O. It is
	// tmux's mutable start directory, not a durable record: `attach -c` rewrites
	// it, so a consumer must have an answer for "this resolves to nothing".
	Dir string
}

// NewSessionWithWindow creates a detached session named name rooted at dir whose
// first window is named window, and returns that window's pane id. It differs
// from NewSession in returning somewhere to send a command: a caller that has to
// run something in, or lay claim to, the session's first pane needs the pane id
// tmux only reports at creation.
func (t *realTmux) NewSessionWithWindow(name, dir, window string) (string, error) {
	out, err := t.run.output("new-session", "-d", "-s", name, "-c", dir, "-n", window, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("create session %q: %w", name, err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", fmt.Errorf("create session %q: tmux returned no pane id", name)
	}
	return id, nil
}

// StampWorkSession records the Work container a session hosts. Both options are
// written together: a session carrying a kind but no id would be a row the
// dashboard could badge but not resolve.
func (t *realTmux) StampWorkSession(session, kind, id string) error {
	if _, err := t.run.output("set-option", "-t", session, optWorkKind, kind); err != nil {
		return fmt.Errorf("stamp work kind on %q: %w", session, err)
	}
	if _, err := t.run.output("set-option", "-t", session, optWorkID, id); err != nil {
		return fmt.Errorf("stamp work id on %q: %w", session, err)
	}
	return nil
}

// WorkSessions lists every live session pop stamped, in one list-sessions
// round-trip. Unstamped sessions are omitted: a consumer asking this question
// wants the Work sessions, and an empty kind is the honest "not one". An
// absent server is an empty list, not an error (ADR-0199 decision 8).
func (t *realTmux) WorkSessions() ([]WorkSession, error) {
	out, err := t.run.output("list-sessions", "-F",
		"#{session_name}\t#{"+optWorkKind+"}\t#{"+optWorkID+"}\t#{session_path}")
	if err != nil {
		if absentServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var sessions []WorkSession
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
			continue
		}
		sessions = append(sessions, WorkSession{Session: parts[0], Kind: parts[1], ID: parts[2], Dir: parts[3]})
	}
	return sessions, nil
}
