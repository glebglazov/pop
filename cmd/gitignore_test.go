package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gitignoreHome = "/home/u"

// gitignoreDeps builds an integrateDeps over a fakeFS with the gitignore-step
// dependencies (gitConfig, getenv) controllable per test. excludesfile, when
// non-empty, is returned for core.excludesfile; otherwise the key is reported
// as unset. xdg, when non-empty, is returned for XDG_CONFIG_HOME.
func gitignoreDeps(fs *fakeFS, stdout *bytes.Buffer, excludesfile, xdg string) *integrateDeps {
	var w io.Writer
	if stdout != nil {
		w = stdout
	}
	d := fakeDeps(gitignoreHome, fs, w)
	d.gitConfig = func(key string) (string, error) {
		if key == "core.excludesfile" && excludesfile != "" {
			return excludesfile, nil
		}
		return "", os.ErrNotExist
	}
	d.getenv = func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}
	return d
}

// TestResolveGitignoreTargetExcludesfileSet: a configured core.excludesfile is
// the target (with leading ~ expanded), regardless of XDG.
func TestResolveGitignoreTargetExcludesfileSet(t *testing.T) {
	fs := newFakeFS()
	d := gitignoreDeps(fs, nil, "~/.gitignore_global", "/xdg")

	got, err := resolveGitignoreTarget(d)
	if err != nil {
		t.Fatalf("resolveGitignoreTarget: %v", err)
	}
	want := filepath.Join(gitignoreHome, ".gitignore_global")
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

// TestResolveGitignoreTargetXDGDefault: with no core.excludesfile but
// XDG_CONFIG_HOME set, the target is $XDG_CONFIG_HOME/git/ignore.
func TestResolveGitignoreTargetXDGDefault(t *testing.T) {
	fs := newFakeFS()
	d := gitignoreDeps(fs, nil, "", "/xdg")

	got, err := resolveGitignoreTarget(d)
	if err != nil {
		t.Fatalf("resolveGitignoreTarget: %v", err)
	}
	want := filepath.Join("/xdg", "git", "ignore")
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

// TestResolveGitignoreTargetHomeDefault: with neither core.excludesfile nor
// XDG_CONFIG_HOME, the target is ~/.config/git/ignore — git's default global
// ignore path. git config is never consulted for a write.
func TestResolveGitignoreTargetHomeDefault(t *testing.T) {
	fs := newFakeFS()
	d := gitignoreDeps(fs, nil, "", "")

	got, err := resolveGitignoreTarget(d)
	if err != nil {
		t.Fatalf("resolveGitignoreTarget: %v", err)
	}
	want := filepath.Join(gitignoreHome, ".config", "git", "ignore")
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

// TestInstallGitignoreAppendsToExcludesfile: the line is appended to an existing
// configured excludes file, preserving prior content and adding a separating
// newline when needed.
func TestInstallGitignoreAppendsToExcludesfile(t *testing.T) {
	fs := newFakeFS()
	excludes := filepath.Join(gitignoreHome, ".gitignore_global")
	fs.files[excludes] = []byte("*.log") // no trailing newline
	var out bytes.Buffer
	d := gitignoreDeps(fs, &out, "~/.gitignore_global", "")

	if err := installGitignore(d, "", "claude"); err != nil {
		t.Fatalf("installGitignore: %v", err)
	}

	got := string(fs.files[excludes])
	want := "*.log\nthoughts/\n"
	if got != want {
		t.Fatalf("excludes content = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), excludes) || !strings.Contains(out.String(), "Added") {
		t.Fatalf("output should report the addition and file, got: %q", out.String())
	}
}

// TestInstallGitignoreCreatesDefaultTarget: with no excludes file the default
// path is created and the line written.
func TestInstallGitignoreCreatesDefaultTarget(t *testing.T) {
	fs := newFakeFS()
	var out bytes.Buffer
	d := gitignoreDeps(fs, &out, "", "")

	if err := installGitignore(d, "", "claude"); err != nil {
		t.Fatalf("installGitignore: %v", err)
	}

	target := filepath.Join(gitignoreHome, ".config", "git", "ignore")
	if got := string(fs.files[target]); got != "thoughts/\n" {
		t.Fatalf("target content = %q, want %q", got, "thoughts/\n")
	}
}

// TestInstallGitignoreIdempotent: a present line results in no write and a clear
// already-configured report.
func TestInstallGitignoreIdempotent(t *testing.T) {
	fs := newFakeFS()
	target := filepath.Join(gitignoreHome, ".config", "git", "ignore")
	fs.files[target] = []byte("node_modules/\nthoughts/\n")
	// Fail any write so a second-write attempt would surface as an error.
	fs.writeErr[target] = errStub
	var out bytes.Buffer
	d := gitignoreDeps(fs, &out, "", "")

	if err := installGitignore(d, "", "claude"); err != nil {
		t.Fatalf("installGitignore (idempotent): %v", err)
	}

	if got := string(fs.files[target]); got != "node_modules/\nthoughts/\n" {
		t.Fatalf("file should be unchanged, got %q", got)
	}
	if !strings.Contains(out.String(), "already configured") {
		t.Fatalf("output should report already-configured, got: %q", out.String())
	}
}

// TestGitignoreConfigured: presence check reports correctly for both target
// resolutions (configured excludesfile and default path), and false for a
// missing file.
func TestGitignoreConfigured(t *testing.T) {
	t.Run("excludesfile configured", func(t *testing.T) {
		fs := newFakeFS()
		excludes := filepath.Join(gitignoreHome, ".gitignore_global")
		fs.files[excludes] = []byte("thoughts/\n")
		d := gitignoreDeps(fs, nil, "~/.gitignore_global", "")

		ok, target, err := gitignoreConfigured(d)
		if err != nil {
			t.Fatalf("gitignoreConfigured: %v", err)
		}
		if !ok {
			t.Fatalf("expected configured=true for %s", excludes)
		}
		if target != excludes {
			t.Fatalf("target = %q, want %q", target, excludes)
		}
	})

	t.Run("default path configured", func(t *testing.T) {
		fs := newFakeFS()
		target := filepath.Join(gitignoreHome, ".config", "git", "ignore")
		fs.files[target] = []byte("thoughts/\n")
		d := gitignoreDeps(fs, nil, "", "")

		ok, got, err := gitignoreConfigured(d)
		if err != nil {
			t.Fatalf("gitignoreConfigured: %v", err)
		}
		if !ok || got != target {
			t.Fatalf("configured=%v target=%q, want true %q", ok, got, target)
		}
	})

	t.Run("missing file not configured", func(t *testing.T) {
		fs := newFakeFS()
		d := gitignoreDeps(fs, nil, "", "")

		ok, target, err := gitignoreConfigured(d)
		if err != nil {
			t.Fatalf("gitignoreConfigured: %v", err)
		}
		if ok {
			t.Fatalf("expected not-configured for missing file")
		}
		want := filepath.Join(gitignoreHome, ".config", "git", "ignore")
		if target != want {
			t.Fatalf("target = %q, want %q", target, want)
		}
	})
}

// TestReportGitignoreRemovalReportOnly: removal prints the line and file for
// manual action and performs no edit.
func TestReportGitignoreRemovalReportOnly(t *testing.T) {
	fs := newFakeFS()
	target := filepath.Join(gitignoreHome, ".config", "git", "ignore")
	fs.files[target] = []byte("thoughts/\n")
	var out bytes.Buffer
	d := gitignoreDeps(fs, &out, "", "")

	if err := reportGitignoreRemoval(d); err != nil {
		t.Fatalf("reportGitignoreRemoval: %v", err)
	}

	if got := string(fs.files[target]); got != "thoughts/\n" {
		t.Fatalf("file must not be edited, got %q", got)
	}
	if !strings.Contains(out.String(), target) || !strings.Contains(out.String(), gitignoreLine) {
		t.Fatalf("output should name the file and line, got: %q", out.String())
	}
}

// TestReportGitignoreRemovalNotConfigured: when the line is absent, removal says
// there is nothing to remove and makes no edit.
func TestReportGitignoreRemovalNotConfigured(t *testing.T) {
	fs := newFakeFS()
	var out bytes.Buffer
	d := gitignoreDeps(fs, &out, "", "")

	if err := reportGitignoreRemoval(d); err != nil {
		t.Fatalf("reportGitignoreRemoval: %v", err)
	}
	if len(fs.files) != 0 {
		t.Fatalf("no file should be created: %v", sortedKeys(fs.files))
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Fatalf("output should report nothing to remove, got: %q", out.String())
	}
}

// TestRunIntegrateComponentsWorkloadGitignore: `pop integrate <agent>
// --workload-gitignore` applies the step (and the core wiring) non-interactively.
func TestRunIntegrateComponentsWorkloadGitignore(t *testing.T) {
	fs := newFakeFS()
	d := gitignoreDeps(fs, nil, "", "")

	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentWorkloadGitignore}, false); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	if _, ok := fs.files[filepath.Join(gitignoreHome, ".claude", "settings.json")]; !ok {
		t.Fatalf("core status wiring not installed")
	}
	target := filepath.Join(gitignoreHome, ".config", "git", "ignore")
	if got := string(fs.files[target]); got != "thoughts/\n" {
		t.Fatalf("gitignore line not applied: %q", got)
	}
}

var errStub = &stubError{}

type stubError struct{}

func (*stubError) Error() string { return "stub write error" }
