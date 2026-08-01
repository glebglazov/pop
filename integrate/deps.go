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

// issueTrackerDocPath returns the Issue tracker doc Shipped-asset path at
// ${XDG_DATA_HOME:-~/.local/share}/pop/agents/docs/issue-tracker.md (ADR-0169),
// mirroring the user-level `~/.agents/docs/` layout. Resolution goes through the
// deps data-dir seam so tests can redirect it into a fake FS.
func issueTrackerDocPath(d *Deps) (string, error) {
	dataDir, err := d.dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "agents", "docs", "issue-tracker.md"), nil
}

// userIssueTrackerDocLinkPath returns the vendor-neutral user-level Issue
// tracker doc path at ~/.agents/docs/issue-tracker.md (ADR-0169). The location
// is hardcoded off the home directory — ADR-0169 rejected an env override.
func userIssueTrackerDocLinkPath(d *Deps) (string, error) {
	home, err := d.userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "docs", "issue-tracker.md"), nil
}

// linkUserIssueTrackerDoc publishes the user-level second layer by symlinking
// ~/.agents/docs/issue-tracker.md at pop's Shipped asset. Creation is strictly
// create-if-absent: anything already at the link path — regular file, directory,
// or symlink pointing anywhere, dangling or not — is the user's and is left
// alone. A regular file occupying ~/.agents/docs aborts the step entirely.
//
// This is the narrow ADR-0169 exception to ADR-0150: pop writes a link, never
// content, outside its data dir, and only into empty space. Every failure is
// logged and skipped so a read-only home still integrates.
func linkUserIssueTrackerDoc(d *Deps) *Outcome {
	target, err := issueTrackerDocPath(d)
	if err != nil {
		debug.Error("linkUserIssueTrackerDoc: asset path: %v", err)
		return nil
	}
	link, err := userIssueTrackerDocLinkPath(d)
	if err != nil {
		debug.Error("linkUserIssueTrackerDoc: link path: %v", err)
		return nil
	}

	if _, err := d.lstatMode(link); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		debug.Error("linkUserIssueTrackerDoc: lstat %s: %v", link, err)
		return nil
	}

	dir := filepath.Dir(link)
	if mode, err := d.lstatMode(dir); err == nil {
		if mode.IsRegular() {
			debug.Error("linkUserIssueTrackerDoc: %s is a regular file; skipping", dir)
			return nil
		}
	} else if !os.IsNotExist(err) {
		debug.Error("linkUserIssueTrackerDoc: lstat %s: %v", dir, err)
		return nil
	}

	if err := d.mkdirAll(dir, 0o755); err != nil {
		debug.Error("linkUserIssueTrackerDoc: mkdir %s: %v", dir, err)
		return nil
	}
	if err := d.symlink(target, link); err != nil {
		debug.Error("linkUserIssueTrackerDoc: symlink %s -> %s: %v", link, target, err)
		return nil
	}
	return &Outcome{Skill: link, Label: "linked"}
}

// staleDataDirWorkStoreDocPath returns the pre-ADR-0169 Shipped-asset path at
// ${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md. Nothing reads this file
// anymore; Integration refresh deletes it unconditionally when present.
func staleDataDirWorkStoreDocPath(d *Deps) (string, error) {
	dataDir, err := d.dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "work-store.md"), nil
}

// popConfigDirWith returns pop's config directory root, respecting
// XDG_CONFIG_HOME through the integrate deps seam.
func popConfigDirWith(d *Deps) (string, error) {
	if xdg := getenv(d, "XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop"), nil
	}
	home, err := d.userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pop"), nil
}

// legacyWorkStoreDocPath returns the pre-ADR-0150 Work store doc path at
// ${XDG_CONFIG_HOME:-~/.config}/pop/work-store.md. Nothing reads this file
// anymore; Integration refresh deletes it unconditionally when present.
func legacyWorkStoreDocPath(d *Deps) (string, error) {
	configDir, err := popConfigDirWith(d)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "work-store.md"), nil
}

// removeLegacyWorkStoreDoc deletes the config-dir Work store doc when present,
// unconditionally and without byte comparison (ADR-0150). Returns one Integrate
// outcome naming the removed path, or nil when the file is absent or removal
// fails — refresh callers must not treat a removal failure as fatal.
func removeLegacyWorkStoreDoc(d *Deps) *Outcome {
	return removeStaleDoc(d, legacyWorkStoreDocPath, "removeLegacyWorkStoreDoc")
}

// removeStaleDataDirWorkStoreDoc deletes the pre-ADR-0169 data-dir Work store
// doc when present, with the same non-fatal contract as the config-dir removal.
func removeStaleDataDirWorkStoreDoc(d *Deps) *Outcome {
	return removeStaleDoc(d, staleDataDirWorkStoreDocPath, "removeStaleDataDirWorkStoreDoc")
}

// removeStaleDoc deletes one superseded doc path when present, unconditionally
// and without byte comparison. Returns one Integrate outcome naming the removed
// path, or nil when the file is absent or removal fails.
func removeStaleDoc(d *Deps, resolve func(*Deps) (string, error), what string) *Outcome {
	path, err := resolve(d)
	if err != nil {
		debug.Error("%s: path: %v", what, err)
		return nil
	}
	if _, err := d.readFile(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		debug.Error("%s: read %s: %v", what, path, err)
		return nil
	}
	if err := d.removeAll(path); err != nil {
		debug.Error("%s: remove %s: %v", what, path, err)
		return nil
	}
	return &Outcome{Skill: path, Label: "removed"}
}

// seedIssueTrackerDoc writes the embedded Issue tracker doc to its Shipped-asset
// path whenever on-disk bytes differ from the embedded copy (ADR-0150,
// ADR-0169). A matching file is left untouched. Agent-agnostic: callers invoke
// it once per Integration refresh, not once per agent.
func seedIssueTrackerDoc(d *Deps) error {
	path, err := issueTrackerDocPath(d)
	if err != nil {
		return err
	}
	existing, err := d.readFile(path)
	if err == nil {
		if bytes.Equal(existing, issueTrackerDoc) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := d.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return d.writeFile(path, issueTrackerDoc, 0o644)
}
