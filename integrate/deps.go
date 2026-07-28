package integrate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
)

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

	// stdin is the wizard's prompt input. Production uses os.Stdin; tests
	// supply a scripted reader. Nil disables prompting (declines every step),
	// which keeps the dry-run/refresh deps inert.
	stdin io.Reader

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

	// Dry-run mode: set DryRun=true to turn writeFile into a comparator.
	// `installed` and `changed` are output fields filled in during the run.
	DryRun    bool
	changed   bool
	installed bool

	// Explicit-install conflict overwrite (ADR 0011): when overwriteConflicts is
	// true, unowned entries may be destroyed after per-item confirmation
	// (assumeYes or interactive prompt). Refresh never sets these fields.
	overwriteConflicts bool
	assumeYes          bool
	interactive        bool
	agentName          string

	// overwrotePaths records agent-location paths hard-deleted during an
	// overwrite-conflicts run; used for outcome labelling.
	overwrotePaths []string

	// prunedStale records resolved install names removed during the latest
	// file-based install; drives removed (stale) outcome lines.
	prunedStale []string
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
		stdin:       os.Stdin,
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

// DryRunDeps returns an Deps that reports what would change
// on disk without performing any writes. See the Deps doc for the
// semantics of `installed` and `changed`.
func DryRunDeps() *Deps {
	return WithDryRun(DefaultDeps())
}

// WithDryRun wraps a base Deps with dry-run behavior. It is exposed
// as a separate function so tests can layer dry-run on top of a fake FS
// without touching the real filesystem.
func WithDryRun(base *Deps) *Deps {
	d := &Deps{
		userHomeDir:  base.userHomeDir,
		readFile:     base.readFile,
		dataDir:      base.dataDir,
		logf:         base.logf,
		skillsPrefix: base.skillsPrefix,
		readDirNames: base.readDirNames,
		DryRun:       true,
	}
	// File-component refresh inspects the link installer's render tree and the
	// agent-location symlinks to decide installed/stale/conflict, so the dry-run
	// deps pass through the base's read-only link seams (readlink, lstatMode,
	// dataDir is already copied above). symlink is the sole write op on this
	// path and stays a no-op — checks never create links, and any real refresh
	// runs through the separate real deps.
	d.symlink = func(string, string) error { return nil }
	d.readlink = base.readlink
	d.lstatMode = base.lstatMode
	// writeFile compares the proposed bytes against what's on disk.
	// Existing file → installed; different content → changed.
	// Missing file → neither (creating new files on an agent that isn't
	// installed yet is not an "update"; the auto-updater should skip).
	d.writeFile = func(path string, data []byte, _ os.FileMode) error {
		existing, err := d.readFile(path)
		if err == nil {
			d.installed = true
			if !bytes.Equal(existing, data) {
				d.changed = true
			}
		}
		return nil
	}
	// mkdirAll is a no-op in dry-run; directory creation is not a meaningful
	// change signal on its own (the file write inside catches the real state).
	d.mkdirAll = func(string, os.FileMode) error { return nil }
	// removeAll is a no-op in dry-run. We intentionally do not probe the
	// target with os.Stat here so the dry-run path stays injectable by
	// tests (which swap readFile/writeFile on a fake FS but do not stub
	// os.Stat). In practice, installed/changed detection relies on the
	// writeFile comparator — every install step that removes a directory
	// is followed by writes into that directory, which provide the signal.
	d.removeAll = func(string) error { return nil }
	// Suppress output from install functions during dry-run.
	d.stdout = nil
	return d
}

