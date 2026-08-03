package tasks

import (
	"io"

	"github.com/glebglazov/pop/internal/tty"
)

// promptReader is the one terminal-aware reader every gate prompt in a run
// shares (see ensurePromptReader). The mechanism — claim the terminal foreground
// before each read, block SIGTTIN/SIGTTOU while reading it — lives in
// internal/tty because the Routine gates need the same guarantee.
type promptReader = tty.Reader

func newPromptReader(in io.Reader) *promptReader {
	return tty.NewReader(in)
}

// promptWarner routes the reader's terminal diagnostics into the gate's own
// output, so a foreground Pop had to wrestle back — or could not — is reported
// where the human is already reading the menu.
func promptWarner(out io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		outputFor(out).line(ansiYellow, format, args...)
	}
}
