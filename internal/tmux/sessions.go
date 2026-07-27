package tmux

import (
	"strconv"
	"strings"

	"github.com/glebglazov/pop/debug"
)

func (t *realTmux) Sessions() ([]SessionActivity, error) {
	out, err := t.run.output("list-sessions", "-F", "#{session_name}\t#{session_activity}")
	if err != nil {
		return nil, err
	}
	return parseSessions(out), nil
}

// CurrentSession returns the session name of the client displaying the caller.
// A tmux error (e.g. not inside a session) surfaces so the caller can treat it
// as "no session".
func (t *realTmux) CurrentSession() (string, error) {
	return t.run.output("display-message", "-p", "#S")
}

// parseSessions turns tmux's list-sessions output into typed structs. Lines
// are "name\tactivity"; the tab delimiter keeps session names containing
// spaces (e.g. a disambiguated "rails (work)") intact.
func parseSessions(out string) []SessionActivity {
	if out == "" {
		return nil
	}

	var sessions []SessionActivity
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			debug.Error("tmux.Sessions: parse timestamp %q: %v", parts[1], err)
		}
		sessions = append(sessions, SessionActivity{Name: parts[0], Activity: ts})
	}
	return sessions
}
