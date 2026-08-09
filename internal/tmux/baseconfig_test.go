package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withIsolatedXDG points HOME / XDG_CONFIG_HOME / XDG_DATA_HOME at empty
// temp dirs so base-config tests never see the developer's real tmux files
// or write into a real data dir.
func withIsolatedXDG(t *testing.T) (home, configHome, dataHome string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	configHome = filepath.Join(root, "config")
	dataHome = filepath.Join(root, "data")
	for _, dir := range []string{home, configHome, dataHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	return home, configHome, dataHome
}

// withUserTmuxConfig isolates XDG and drops a classic ~/.tmux.conf so NewSession
// argv assertions stay free of -f / list-sessions probes.
func withUserTmuxConfig(t *testing.T) {
	t.Helper()
	home, _, _ := withIsolatedXDG(t)
	if err := os.WriteFile(filepath.Join(home, ".tmux.conf"), []byte("set -g status on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserHasTmuxConfig(t *testing.T) {
	t.Run("neither path", func(t *testing.T) {
		withIsolatedXDG(t)
		if userHasTmuxConfig() {
			t.Fatal("expected no user config")
		}
	})

	t.Run("classic ~/.tmux.conf", func(t *testing.T) {
		home, _, _ := withIsolatedXDG(t)
		if err := os.WriteFile(filepath.Join(home, ".tmux.conf"), []byte("#\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !userHasTmuxConfig() {
			t.Fatal("expected classic path to count")
		}
	})

	t.Run("XDG tmux/tmux.conf without classic", func(t *testing.T) {
		_, configHome, _ := withIsolatedXDG(t)
		dir := filepath.Join(configHome, "tmux")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tmux.conf"), []byte("#\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !userHasTmuxConfig() {
			t.Fatal("expected XDG path to count even when ~/.tmux.conf is absent")
		}
	})
}

func TestRenderBaseConfigWritesUnderDataDirNotHome(t *testing.T) {
	home, configHome, dataHome := withIsolatedXDG(t)

	path, err := renderBaseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "pop", baseConfigRelPath)
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), string(baseConfSource)) {
		t.Fatal("rendered body must start with the embed")
	}
	// Decision 5: nothing under the user's home / config tree.
	if entries, err := os.ReadDir(home); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("home entries = %v, want empty", entries)
	}
	if entries, err := os.ReadDir(configHome); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("config home entries = %v, want empty", entries)
	}
}

func TestBaseConfigOnlySetOptionAndBindKey(t *testing.T) {
	for i, line := range strings.Split(string(baseConfSource), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "set-option ") || strings.HasPrefix(line, "bind-key ") {
			continue
		}
		t.Fatalf("line %d %q: base config may only contain set-option and bind-key", i+1, line)
	}
}

func TestBaseConfigShipsPopBindingsAndNoviceDefaults(t *testing.T) {
	body := string(baseConfSource)
	for _, want := range []string{
		"set-option -g prefix ",
		"set-option -g mouse on",
		"set-option -g mode-keys vi",
		"set-option -g status",
		"detach",
		"bind-key p display-popup",
		"pop project dashboard",
		"bind-key P display-popup",
		"pop worktree dashboard",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("base config missing %q", want)
		}
	}
}

func TestNewSessionPassesBaseConfigWhenStartingWithoutUserConfig(t *testing.T) {
	_, _, dataHome := withIsolatedXDG(t)
	Version = "2026.8.0"
	t.Cleanup(func() { Version = "" })
	r := &recordingRunner{
		responses: []runnerResponse{
			{err: fmt.Errorf("no server running on /tmp/tmux-501/default")},
			{out: ""},
			{out: ""}, // set-option -s @pop_version
		},
	}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	rendered := filepath.Join(dataHome, "pop", baseConfigRelPath)
	want := [][]string{
		{"list-sessions"},
		{"-f", rendered, "new-session", "-ds", "work", "-c", "/proj"},
		{"set-option", "-s", optBaseVersion, "2026.8.0"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
	if _, err := os.Stat(rendered); err != nil {
		t.Fatalf("expected rendered base config at %s: %v", rendered, err)
	}
}

func TestNewSessionOmitsBaseConfigWhenUserConfigExists(t *testing.T) {
	withUserTmuxConfig(t)
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"new-session", "-ds", "work", "-c", "/proj"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestNewSessionOmitsBaseConfigWhenXDGUserConfigExists(t *testing.T) {
	_, configHome, _ := withIsolatedXDG(t)
	dir := filepath.Join(configHome, "tmux")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmux.conf"), []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.calls {
		for _, arg := range call {
			if arg == "-f" {
				t.Fatalf("must not pass -f when XDG user config exists; calls = %v", r.calls)
			}
		}
	}
}

func TestNewSessionOmitsBaseConfigWhenServerAlreadyListening(t *testing.T) {
	withIsolatedXDG(t)
	Version = "2026.8.0"
	t.Cleanup(func() { Version = "" })
	r := &recordingRunner{
		responses: []runnerResponse{
			{out: "other\t1"},                        // list-sessions → server live
			{err: fmt.Errorf("invalid option: @pop_version")}, // unstamped
			{out: ""}, // new-session
		},
	}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"list-sessions"},
		{"show-options", "-sv", optBaseVersion},
		{"new-session", "-ds", "work", "-c", "/proj"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestNewSessionDoesNotReadUserConfig(t *testing.T) {
	// A user config that is unreadable must still suppress -f via existence
	// alone — Stat succeeds on a mode-0000 file we own; we must not Open it.
	home, _, _ := withIsolatedXDG(t)
	path := filepath.Join(home, ".tmux.conf")
	if err := os.WriteFile(path, []byte("this must not be read\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"new-session", "-ds", "work", "-c", "/proj"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v (existence must suppress -f without reading)", r.calls, want)
	}
}

func TestNewSessionWithWindowPassesBaseConfigWhenStartingWithoutUserConfig(t *testing.T) {
	_, _, dataHome := withIsolatedXDG(t)
	Version = "2026.8.1"
	t.Cleanup(func() { Version = "" })
	r := &recordingRunner{
		responses: []runnerResponse{
			{err: fmt.Errorf("error connecting to /tmp/tmux-501/pop")},
			{out: "%9\n"},
			{out: ""}, // stamp
		},
	}
	tm := &realTmux{run: r}

	paneID, err := tm.NewSessionWithWindow("s", "/repo", "map")
	if err != nil {
		t.Fatal(err)
	}
	if paneID != "%9" {
		t.Fatalf("paneID = %q, want %%9", paneID)
	}
	rendered := filepath.Join(dataHome, "pop", baseConfigRelPath)
	want := [][]string{
		{"list-sessions"},
		{"-f", rendered, "new-session", "-d", "-s", "s", "-c", "/repo", "-n", "map", "-P", "-F", "#{pane_id}"},
		{"set-option", "-s", optBaseVersion, "2026.8.1"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestRenderBaseConfigSourcesIncludeLastGuarded(t *testing.T) {
	_, configHome, _ := withIsolatedXDG(t)
	wantInclude := filepath.Join(configHome, "pop", "tmux.conf")

	path, err := renderBaseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.HasPrefix(text, string(baseConfSource)) {
		t.Fatal("embed must come first")
	}
	idx := strings.Index(text, "if-shell -F")
	if idx < 0 {
		t.Fatal("rendered base must contain if-shell -F existence guard")
	}
	if idx < len(baseConfSource) {
		t.Fatal("include stanza must follow the embed, not sit inside it")
	}
	if !strings.Contains(text[idx:], "source-file -q '"+wantInclude+"'") {
		t.Fatalf("include stanza must source-file -q the default path %q; body:\n%s", wantInclude, text[idx:])
	}
}

func TestRenderBaseConfigHonoursCustomIncludePath(t *testing.T) {
	withIsolatedXDG(t)
	custom := filepath.Join(t.TempDir(), "chezmoi", "pop-tmux.conf")
	path, err := renderBaseConfig(custom)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "source-file -q '"+custom+"'") {
		t.Fatalf("custom include path missing from render:\n%s", body)
	}
}

func TestRenderBaseConfigNeverWritesIncludeFile(t *testing.T) {
	_, configHome, _ := withIsolatedXDG(t)
	includePath := filepath.Join(configHome, "pop", "tmux.conf")

	if _, err := renderBaseConfig(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(includePath); !os.IsNotExist(err) {
		t.Fatalf("render must not create the include file; stat err = %v", err)
	}

	// Pre-existing include content must survive regeneration.
	if err := os.MkdirAll(filepath.Dir(includePath), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "bind-key X display-message 'mine'\n"
	if err := os.WriteFile(includePath, []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := renderBaseConfig(""); err != nil {
		t.Fatal(err)
	}
	if _, err := renderBaseConfig(""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(includePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != marker {
		t.Fatalf("regenerating base must leave include untouched; got %q", got)
	}
}

func TestNewSessionStampsOnlyWhenBaseConfigSupplied(t *testing.T) {
	Version = "2026.8.0"
	t.Cleanup(func() { Version = "" })

	t.Run("stamps after -f start", func(t *testing.T) {
		withIsolatedXDG(t)
		r := &recordingRunner{
			responses: []runnerResponse{
				{err: fmt.Errorf("no server running")},
				{out: ""},
				{out: ""},
			},
		}
		tm := &realTmux{run: r}
		if err := tm.NewSession("work", "/proj"); err != nil {
			t.Fatal(err)
		}
		last := r.calls[len(r.calls)-1]
		want := []string{"set-option", "-s", optBaseVersion, "2026.8.0"}
		if !reflect.DeepEqual(last, want) {
			t.Fatalf("last call = %v, want stamp %v", last, want)
		}
	})

	t.Run("started with user config stays unstamped", func(t *testing.T) {
		withUserTmuxConfig(t)
		r := &recordingRunner{}
		tm := &realTmux{run: r}
		if err := tm.NewSession("work", "/proj"); err != nil {
			t.Fatal(err)
		}
		for _, call := range r.calls {
			if len(call) >= 3 && call[0] == "set-option" && call[2] == optBaseVersion {
				t.Fatalf("must not stamp when user config suppressed -f; calls = %v", r.calls)
			}
			for _, arg := range call {
				if arg == "source-file" {
					t.Fatalf("must not source-file when user has own config; calls = %v", r.calls)
				}
			}
		}
	})
}

func TestRefreshResourcesWhenVersionExceedsStamp(t *testing.T) {
	_, _, dataHome := withIsolatedXDG(t)
	Version = "2026.9.0"
	t.Cleanup(func() { Version = "" })
	rendered := filepath.Join(dataHome, "pop", baseConfigRelPath)
	r := &recordingRunner{
		responses: []runnerResponse{
			{out: "other\t1"},     // list-sessions → live
			{out: "2026.8.0"},     // show-options stamp
			{out: ""},             // source-file
			{out: ""},             // re-stamp
			{out: ""},             // new-session
		},
	}
	tm := &realTmux{run: r}
	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"list-sessions"},
		{"show-options", "-sv", optBaseVersion},
		{"source-file", rendered},
		{"set-option", "-s", optBaseVersion, "2026.9.0"},
		{"new-session", "-ds", "work", "-c", "/proj"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestRefreshSkipsEqualStamp(t *testing.T) {
	withIsolatedXDG(t)
	Version = "2026.8.0"
	t.Cleanup(func() { Version = "" })
	r := &recordingRunner{
		responses: []runnerResponse{
			{out: "other\t1"},
			{out: "2026.8.0"},
			{out: ""},
		},
	}
	tm := &realTmux{run: r}
	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.calls {
		if len(call) > 0 && call[0] == "source-file" {
			t.Fatalf("equal stamp must not re-source; calls = %v", r.calls)
		}
	}
}

func TestUnstampedLiveServerNeverSourced(t *testing.T) {
	// Started-but-not-configured: pop started the server for a user who has
	// their own tmux config (no -f, no stamp). A later run must not source
	// pop's bindings into that server (ADR-0199 decision 7).
	withIsolatedXDG(t)
	Version = "2026.9.0"
	t.Cleanup(func() { Version = "" })
	r := &recordingRunner{
		responses: []runnerResponse{
			{out: "other\t1"},
			{err: fmt.Errorf("invalid option: @pop_version")},
			{out: ""},
		},
	}
	tm := &realTmux{run: r}
	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.calls {
		if len(call) > 0 && call[0] == "source-file" {
			t.Fatalf("unstamped server must never be sourced into; calls = %v", r.calls)
		}
		if len(call) >= 3 && call[0] == "set-option" && call[2] == optBaseVersion {
			t.Fatalf("unstamped live server must not gain a stamp on later new-session; calls = %v", r.calls)
		}
	}
}

func TestVersionExceeds(t *testing.T) {
	cases := []struct {
		current, stamped string
		want             bool
	}{
		{"2026.9.0", "2026.8.0", true},
		{"2026.8.0", "2026.8.0", false},
		{"2026.8.0", "2026.9.0", false},
		{"2026.10.0", "2026.9.0", true},
		{"abc", "def", true},
		{"dev", "dev", false},
		{"", "2026.8.0", false},
		{"2026.8.0", "", false},
	}
	for _, c := range cases {
		if got := versionExceeds(c.current, c.stamped); got != c.want {
			t.Errorf("versionExceeds(%q, %q) = %v, want %v", c.current, c.stamped, got, c.want)
		}
	}
}
