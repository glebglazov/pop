package dashboardshell

import (
	"fmt"
	"os"

	"github.com/glebglazov/pop/config"
)

// The config the pages hold is re-read when the files it was read from change on
// disk (ADR-0242 decision 5, amending ADR-0202 decision 14). `pop config` run in
// another pane, or a hand edit of config.toml, used to need a dashboard restart
// to be seen; now each poll stats the files and a changed mtime re-reads through
// the same reconciliation the Config modal's own post-write re-read uses, so a
// newly configured project's sets appear and a removed preset falls back with the
// filter state intact.

// configWatch is the stat fingerprint of those files, remembered between polls.
// It is held by pointer because the shell is a value model: every copy of Shell
// bubbletea makes must stat against the same remembered mtimes, or a change would
// be reported over and over by whichever copy had not seen it yet.
type configWatch struct {
	paths []string
	seen  map[string]string
}

// newConfigWatch remembers the files as they are now, so the first poll after the
// dashboard opens reports no change — the config the shell already holds was read
// from exactly this state.
func newConfigWatch(paths ...string) *configWatch {
	w := &configWatch{paths: paths, seen: make(map[string]string, len(paths))}
	w.changed()
	return w
}

// changed stats every watched file and reports whether any of them differs from
// what the last poll saw. A file that does not exist has a fingerprint of its
// own, so a config file appearing or being deleted counts as a change too.
func (w *configWatch) changed() bool {
	if w == nil {
		return false
	}
	changed := false
	for _, path := range w.paths {
		fp := statFingerprint(path)
		if w.seen[path] != fp {
			w.seen[path] = fp
			changed = true
		}
	}
	return changed
}

// statFingerprint is one stat call rendered as a string: mtime and size, which
// together catch every edit a human or `pop config` makes. Reading the file to
// compare its bytes would be a whole config parse per poll for the answer "no".
func statFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", info.ModTime().UnixNano(), info.Size())
}

// watchedConfigPaths is the hand-authored config the shell loaded plus the
// override layer pop writes itself — the two files a config change lands in.
func watchedConfigPaths(cfgPath string) []string {
	return []string{cfgPath, config.DefaultOverrideConfigPath()}
}
