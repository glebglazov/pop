package integrate

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
)

// Deps holds injection closures only: filesystem, IO, symlink, and config
// seams, plus the overwrite-confirmation callback. Per-invocation mode flags
// and run outcomes live on Request / Report.
type Deps struct {
	// getenv resolves XDG_DATA_HOME / XDG_CONFIG_HOME for cmd-local paths
	// (ADR-0145). Nil falls back to the cmd-layer FS seam.
	getenv      func(string) string
	userHomeDir func() (string, error)
	readFile    func(string) ([]byte, error)
	writeFile   func(string, []byte, os.FileMode) error
	mkdirAll    func(string, os.FileMode) error
	removeAll   func(string) error
	stdout      io.Writer

	// logf emits a debug log line. Production wires debug.Log; tests can
	// override to capture what was logged without needing POP_LOG set.
	logf func(string, ...any)

	// File-based component installer (link installer, ADR 0011). dataDir
	// resolves pop's data directory root (the parent of integrations/);
	// symlink/readlink/lstatMode manage the agent-location symlinks and the
	// ownership check.
	dataDir   func() (string, error)
	symlink   func(target, link string) error
	readlink  func(string) (string, error)
	lstatMode func(string) (os.FileMode, error)

	// readDirNames lists immediate entry names under a directory. Used by
	// stale-name cleanup after prefix or base-name changes (ADR 0063).
	readDirNames func(string) ([]string, error)

	// skillsPrefix is the resolved skill-name prefix for rendered skills (the
	// `<prefix>` in `<prefix><base>`, ADR 0063). A nil pointer means "unset" →
	// config.DefaultSkillsPrefix (`pop-`); a non-nil pointer (including an empty
	// string) is used verbatim, so skills_prefix = "" installs bare base names.
	skillsPrefix *string

	// ConfirmOverwrite is the overwrite-confirmation seam for unowned
	// conflict paths. cmd wires a TTY prompt; tests supply a scripted
	// callback. Nil declines every confirmation.
	ConfirmOverwrite func(path string) bool
}

func DefaultDeps() *Deps {
	d := &Deps{
		userHomeDir: os.UserHomeDir,
		readFile:    os.ReadFile,
		writeFile:   os.WriteFile,
		mkdirAll:    os.MkdirAll,
		removeAll:   os.RemoveAll,
		stdout:      os.Stdout,
		logf:        debug.Log,
		symlink:     os.Symlink,
		readlink:    os.Readlink,
		lstatMode: func(p string) (os.FileMode, error) {
			fi, err := os.Lstat(p)
			if err != nil {
				return 0, err
			}
			return fi.Mode(), nil
		},
		readDirNames: osReadDirNames,
	}
	d.getenv = func(key string) string { return os.Getenv(key) }
	d.dataDir = func() (string, error) { return popDataDirWith(d) }
	d.skillsPrefix = loadSkillsPrefix()
	return d
}

// TestFS is the filesystem seam TestDeps wires into Deps.
type TestFS interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	RemoveAll(string) error
	Symlink(string, string) error
	Readlink(string) (string, error)
	LstatMode(string) (os.FileMode, error)
	ReadDirNames(string) ([]string, error)
}

// TestDeps constructs integrate deps backed by an in-memory filesystem for tests.
func TestDeps(home string, fs TestFS, stdout io.Writer) *Deps {
	if stdout == nil {
		stdout = io.Discard
	}
	return &Deps{
		userHomeDir:  func() (string, error) { return home, nil },
		getenv:       func(string) string { return "" },
		readFile:     fs.ReadFile,
		writeFile:    fs.WriteFile,
		mkdirAll:     fs.MkdirAll,
		removeAll:    fs.RemoveAll,
		stdout:       stdout,
		logf:         func(string, ...any) {},
		dataDir:      func() (string, error) { return filepath.Join(home, ".local", "share", "pop"), nil },
		symlink:      fs.Symlink,
		readlink:     fs.Readlink,
		lstatMode:    fs.LstatMode,
		readDirNames: fs.ReadDirNames,
	}
}

// GuardReadOnly makes write paths on d fail the test if invoked.
func GuardReadOnly(t testing.TB, d *Deps) {
	t.Helper()
	d.writeFile = func(string, []byte, os.FileMode) error { t.Fatalf("integrate deps wrote a file"); return nil }
	d.mkdirAll = func(string, os.FileMode) error { t.Fatalf("integrate deps created a directory"); return nil }
	d.removeAll = func(string) error { t.Fatalf("integrate deps removed a path"); return nil }
	d.symlink = func(string, string) error { t.Fatalf("integrate deps created a symlink"); return nil }
}

func getenv(d *Deps, key string) string {
	if d != nil && d.getenv != nil {
		return d.getenv(key)
	}
	return os.Getenv(key)
}

// osReadDirNames lists the immediate entry names under dir, sorted. A missing
// directory is not an error — it reports no entries.
func osReadDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// loadSkillsPrefix resolves [integrations] skills_prefix from merged config.
func loadSkillsPrefix() *string {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Log("loadSkillsPrefix: config load failed (%v); using default prefix %q", err, config.DefaultSkillsPrefix)
		return nil
	}
	p := cfg.ResolveSkillsPrefix()
	return &p
}

// resolveSkillsPrefix returns the resolved skill-name prefix for this deps.
func (d *Deps) resolveSkillsPrefix() string {
	if d == nil || d.skillsPrefix == nil {
		return config.DefaultSkillsPrefix
	}
	return *d.skillsPrefix
}

// UserHomeDir returns the home directory through the deps seam.
func (d *Deps) UserHomeDir() (string, error) {
	return d.userHomeDir()
}

// popDataDirWith returns pop's data directory root, respecting XDG_DATA_HOME
// through the integrate deps seam. File-based integration artifacts live under
// <dataDir>/integrations/.
func popDataDirWith(d *Deps) (string, error) {
	if xdg := getenv(d, "XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop"), nil
	}
	home, err := d.userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "pop"), nil
}

// workStoreDocPath returns the machine-global pop Work store doc path,
// ${XDG_CONFIG_HOME:-~/.config}/pop/work-store.md (ADR-0136). The home fallback
// resolves through the deps so tests can redirect it into a fake filesystem.
func workStoreDocPath(d *Deps) (string, error) {
	if xdg := getenv(d, "XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "work-store.md"), nil
	}
	home, err := d.userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pop", "work-store.md"), nil
}

// seedWorkStoreDoc writes the embedded pop Work store doc to its machine-global
// path create-if-absent, and never overwrites an existing (possibly user-edited)
// file — the doc is config, and a user's edits are the machine-global override
// (ADR-0136). Agent-agnostic: callers invoke it once per Integration refresh,
// not once per agent.
func seedWorkStoreDoc(d *Deps) error {
	path, err := workStoreDocPath(d)
	if err != nil {
		return err
	}
	if _, err := d.readFile(path); err == nil {
		return nil // already present — user edits are the override, never overwrite
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := d.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return d.writeFile(path, workStoreDoc, 0o644)
}
