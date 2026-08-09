package tmux

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// baseConfSource is pop's Base tmux config (ADR-0199 decisions 4–5). Embedded
// so a first-time user gets a working, pop-aware server without writing a
// config file of their own. Restricted to set-option / bind-key so a later
// upgrade can re-source it into a live stamped server (decision 7). The
// Tmux config include stanza is appended at render time (decision 6), not
// embedded, so the embed stays re-sourceable without a baked-in path.
//
//go:embed base.conf
var baseConfSource []byte

// baseConfigRelPath is where the regenerated base config lands under pop's
// data dir. Named deliberately not "tmux.conf" — that name belongs to the
// user's own file (glossary: Base tmux config).
const baseConfigRelPath = "tmux/base.conf"

// defaultTmuxIncludePath is the documented default for the Tmux config
// include, matching config.DefaultTmuxIncludePath. Kept as a tilde form so
// expandIncludePath resolves it against the current HOME / XDG_CONFIG_HOME.
const defaultTmuxIncludePath = "~/.config/pop/tmux.conf"

// optBaseVersion is the server-scope stamp recording which pop version
// supplied the Base tmux config (ADR-0199 decision 7). Its presence means
// "pop's config is what runs here" — not merely "pop started this". An
// unstamped server is never sourced into.
const optBaseVersion = "@pop_version"

// Version is the running pop binary's version string (CalVer or describe,
// without a leading "v"). Injected at build time via -ldflags and set from
// cmd at process start; tests assign it directly. Empty stamps as "dev".
var Version string

// stampVersion returns the value written into optBaseVersion.
func stampVersion() string {
	v := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if v == "" {
		return "dev"
	}
	return v
}

// BindingFragment returns pop's embedded Base tmux config source (ADR-0199
// decision 9): the same bytes NewSession renders when supplying -f. Suitable
// for pasting into a user-owned tmux.conf or sourcing into a live server. Pop
// never installs this into a server it did not configure.
func BindingFragment() string {
	return string(baseConfSource)
}

// UserHasTmuxConfig reports whether a user-authored tmux config exists at
// either of tmux's search paths. Existence only — the file is never read
// (ADR-0199 decision 4): a user with their own config is left completely alone.
func UserHasTmuxConfig() bool {
	return userHasTmuxConfig()
}

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

// expandIncludePath turns a configured include path (possibly ~-prefixed or
// empty) into an absolute path for the rendered source-file line. Empty
// means the documented default. Prefer $XDG_CONFIG_HOME/pop/tmux.conf when
// the default is requested and XDG_CONFIG_HOME is set.
func expandIncludePath(configured string) string {
	path := strings.TrimSpace(configured)
	if path == "" || path == defaultTmuxIncludePath {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "pop", "tmux.conf")
		}
		path = defaultTmuxIncludePath
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// includeStanza is the Base tmux config's customization seam (ADR-0199
// decision 6): source the user-authored include last, guarded so an absent
// file is not a startup error. Pop never creates this file.
func includeStanza(absPath string) string {
	// Quote for tmux: single-quoted path with embedded singles broken out.
	q := "'" + strings.ReplaceAll(absPath, "'", "'\"'\"'") + "'"
	// if-shell -F (format condition) wrapping source-file -q: -q is the
	// existence guard — an absent path produces no error and does not
	// block the rest of the config.
	return "\n# Tmux config include (ADR-0199 decision 6) — sourced last; absent file is not an error.\n" +
		"if-shell -F '1' { source-file -q " + q + " }\n"
}

// renderBaseConfig writes the embedded base config plus the include stanza
// into the pop data dir and returns its absolute path. Regenerated on every
// call so the on-disk bytes always match the binary (ADR-0011). Never writes
// under the user's home config tree (ADR-0199 decision 5) and never creates
// or rewrites the include file (decision 6).
func renderBaseConfig(include string) (string, error) {
	dir, err := popDataDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, baseConfigRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create pop tmux data dir: %w", err)
	}
	absInclude := expandIncludePath(include)
	body := append([]byte{}, baseConfSource...)
	body = append(body, []byte(includeStanza(absInclude))...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
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
// (ADR-0199 decision 4). supplied is true only when -f was prepended — the
// caller stamps the server version in that case and only then (decision 7).
// When a server is already listening, a stale stamp triggers a re-source.
// Returns args unchanged when a user config exists (and never reads that
// file): such a start is correctly left unstamped.
func (t *realTmux) withBaseConfigIfStarting(args []string) (out []string, supplied bool, err error) {
	if userHasTmuxConfig() {
		return args, false, nil
	}
	if !t.serverAbsent() {
		if err := t.refreshBaseConfigIfStale(); err != nil {
			return nil, false, err
		}
		return args, false, nil
	}
	include := ""
	if t != nil {
		include = t.include
	}
	path, err := renderBaseConfig(include)
	if err != nil {
		return nil, false, err
	}
	out = make([]string, 0, len(args)+2)
	out = append(out, "-f", path)
	return append(out, args...), true, nil
}

// writeVersionStamp records stampVersion as a server option. Called only
// after a new-session that supplied the base config via -f.
func (t *realTmux) writeVersionStamp() error {
	_, err := t.run.output("set-option", "-s", optBaseVersion, stampVersion())
	if err != nil {
		return fmt.Errorf("stamp base config version: %w", err)
	}
	return nil
}

// refreshBaseConfigIfStale re-sources the regenerated base config when the
// live server carries a stamp older than the running pop version (ADR-0199
// decision 7). An unstamped or absent server is a no-op — never sourced into.
// Once per realTmux instance so Ensure → NewSession does not double-probe.
func (t *realTmux) refreshBaseConfigIfStale() error {
	if t == nil {
		return nil
	}
	t.refreshOnce.Do(func() {
		t.refreshErr = t.doRefreshBaseConfigIfStale()
	})
	return t.refreshErr
}

func (t *realTmux) doRefreshBaseConfigIfStale() error {
	stamped, ok, err := t.readVersionStamp()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	current := stampVersion()
	if !versionExceeds(current, stamped) {
		return nil
	}
	include := t.include
	path, err := renderBaseConfig(include)
	if err != nil {
		return err
	}
	if _, err := t.run.output("source-file", path); err != nil {
		return fmt.Errorf("re-source base tmux config: %w", err)
	}
	return t.writeVersionStamp()
}

// readVersionStamp returns the server's @pop_version value. ok is false when
// the option is unset (unstamped) or the server is absent.
func (t *realTmux) readVersionStamp() (value string, ok bool, err error) {
	out, err := t.run.output("show-options", "-sv", optBaseVersion)
	if err != nil {
		if absentServer(err) || strings.Contains(err.Error(), "invalid option") {
			return "", false, nil
		}
		return "", false, err
	}
	value = strings.TrimSpace(out)
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

// releaseTagPattern matches an exact CalVer release tag (YYYY.M.N), optional
// leading "v". Mirrors release.releaseTagPattern so stamp compares agree with
// the Update check without importing that package into internal/tmux.
var releaseTagPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// versionExceeds reports whether current is newer than stamped. Exact CalVer
// tags compare numerically; any other pair (describe strings, "dev", mixed)
// treats inequality as newer so a rebuilt binary still refreshes.
func versionExceeds(current, stamped string) bool {
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	stamped = strings.TrimPrefix(strings.TrimSpace(stamped), "v")
	if current == "" || stamped == "" || current == stamped {
		return false
	}
	if releaseTagPattern.MatchString(current) && releaseTagPattern.MatchString(stamped) {
		c, cok := parseStampCalVer(current)
		s, sok := parseStampCalVer(stamped)
		if cok && sok {
			return compareStampCalVer(c, s) > 0
		}
	}
	return true
}

type stampCalVer struct{ year, month, counter int }

func parseStampCalVer(version string) (stampCalVer, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !releaseTagPattern.MatchString(v) {
		return stampCalVer{}, false
	}
	parts := strings.Split(v, ".")
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	counter, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return stampCalVer{}, false
	}
	return stampCalVer{year: year, month: month, counter: counter}, true
}

func compareStampCalVer(a, b stampCalVer) int {
	switch {
	case a.year != b.year:
		return cmpInt(a.year, b.year)
	case a.month != b.month:
		return cmpInt(a.month, b.month)
	default:
		return cmpInt(a.counter, b.counter)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
