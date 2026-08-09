package tmux

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// baseConfSource is pop's Base tmux config (ADR-0199 decisions 4–5). Embedded
// so a first-time user gets a working, pop-aware server without writing a
// config file of their own. Restricted to set-option / bind-key so a later
// upgrade can re-source it into a live stamped server (decision 7).
//
//go:embed base.conf
var baseConfSource []byte

// baseConfigRelPath is where the regenerated base config lands under pop's
// data dir. Named deliberately not "tmux.conf" — that name belongs to the
// user's own file (glossary: Base tmux config).
const baseConfigRelPath = "tmux/base.conf"

// userHasTmuxConfig reports whether a user-authored tmux config exists at
// either of tmux's search paths. Existence only — the file is never read
// (ADR-0199 decision 4): a user with their own config is left completely alone.
func userHasTmuxConfig() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if home != "" {
		if fileExists(filepath.Join(home, ".tmux.conf")) {
			return true
		}
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}
	if xdg == "" {
		return false
	}
	return fileExists(filepath.Join(xdg, "tmux", "tmux.conf"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// popDataDir resolves pop's data directory the same way the rest of the
// binary does: $XDG_DATA_HOME/pop, else ~/.local/share/pop.
func popDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for pop data dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "pop"), nil
}

// renderBaseConfig writes the embedded base config into the pop data dir and
// returns its absolute path. Regenerated on every call so the on-disk bytes
// always match the binary (ADR-0011). Never writes under the user's home
// config tree (ADR-0199 decision 5).
func renderBaseConfig() (string, error) {
	dir, err := popDataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, baseConfigRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create pop tmux data dir: %w", err)
	}
	if err := os.WriteFile(path, baseConfSource, 0o644); err != nil {
		return "", fmt.Errorf("render base tmux config: %w", err)
	}
	return path, nil
}

// serverAbsent reports whether the addressed socket has no listening server.
// Used to gate -f: tmux loads a configuration file only when the server
// process starts, so passing -f against a live server is a no-op we skip.
func (t *realTmux) serverAbsent() bool {
	_, err := t.run.output("list-sessions")
	return absentServer(err)
}

// withBaseConfigIfStarting prepends -f <rendered-base> when this new-session
// will start a server and the user has no tmux config of their own
// (ADR-0199 decision 4). Returns args unchanged when a user config exists
// (and never reads that file) or when a server is already listening.
func (t *realTmux) withBaseConfigIfStarting(args []string) ([]string, error) {
	if userHasTmuxConfig() {
		return args, nil
	}
	if !t.serverAbsent() {
		return args, nil
	}
	path, err := renderBaseConfig()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-f", path)
	return append(out, args...), nil
}
