package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points the default agent home at a temporary directory, so no test
// can clear or install into the human's own pinned home.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "pop-eval-agent-home-*")
	if err != nil {
		panic(err)
	}
	defaultAgentHomeRoot = root
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

func testAgentHome(t *testing.T) agentHome {
	t.Helper()
	home, err := newAgentHome(filepath.Join(t.TempDir(), "agent-home"))
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestAgentHomePrepareKeepsLoginsAndClearsSessionState(t *testing.T) {
	home := testAgentHome(t)
	if err := home.prepare(); err != nil {
		t.Fatal(err)
	}
	claude, codex := filepath.Join(home.root, "claude"), filepath.Join(home.root, "codex")
	logins := map[string]string{
		filepath.Join(claude, ".credentials.json"): "claude login",
		filepath.Join(claude, ".claude.json"):      "claude account",
		filepath.Join(codex, "auth.json"):          "codex login",
	}
	for path, body := range logins {
		writeFile(t, path, body)
	}
	sessionState := []string{
		filepath.Join(claude, "projects", "-tmp-trial", "session.jsonl"),
		filepath.Join(claude, "projects", "-tmp-trial", "memory", "MEMORY.md"),
		filepath.Join(claude, "todos", "todo.json"),
		filepath.Join(claude, "CLAUDE.md"),
		filepath.Join(claude, "plugins", "installed_plugins.json"),
		filepath.Join(codex, "sessions", "2026", "rollout.jsonl"),
		filepath.Join(codex, "history.jsonl"),
		filepath.Join(codex, "AGENTS.md"),
	}
	for _, path := range sessionState {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, "earlier session")
	}
	writeFile(t, filepath.Join(claude, "settings.json"), `{"enabledPlugins":{"pstack":true}}`)

	if err := home.prepare(); err != nil {
		t.Fatal(err)
	}
	for path, body := range logins {
		if got, err := os.ReadFile(path); err != nil || string(got) != body {
			t.Fatalf("login %s = %q, %v; want it kept as %q", path, got, err, body)
		}
	}
	for _, path := range sessionState {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("session state %s survived the clear: %v", path, err)
		}
	}
	for _, name := range []string{"claude/settings.json", "codex/config.toml"} {
		want, err := checkedInAgentHome.ReadFile("agent-home/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(home.root, name)); err != nil || string(got) != string(want) {
			t.Fatalf("%s = %q, %v; want the checked-in file", name, got, err)
		}
	}
}

func TestAgentHomeRefusesToClearADirectoryItDidNotMake(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, "claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, own, "the human's own instructions")
	home, err := newAgentHome(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := home.prepare(); err == nil || !strings.Contains(err.Error(), "was not made by the harness") {
		t.Fatalf("prepare error = %v", err)
	}
	if got, err := os.ReadFile(own); err != nil || string(got) != "the human's own instructions" {
		t.Fatalf("refused directory changed: %q, %v", got, err)
	}
}

func TestAgentHomePreflightNamesTheLoginCommand(t *testing.T) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "claude"), `#!/bin/sh
[ "$1 $2" = "auth status" ] || exit 9
if [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ] || [ -n "$CLAUDE_CODE_OAUTH_TOKEN" ]; then echo '{"loggedIn":true}'; exit 0; fi
echo '{"loggedIn":false}'; exit 1
`)
	writeFile(t, filepath.Join(bin, "codex"), `#!/bin/sh
[ "$1 $2" = "login status" ] || exit 9
[ -f "$CODEX_HOME/auth.json" ] && { echo 'Logged in'; exit 0; }
echo 'Not logged in'; exit 1
`)
	for _, name := range []string{"claude", "codex"} {
		if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	home := testAgentHome(t)
	claude, codex := filepath.Join(home.root, "claude"), filepath.Join(home.root, "codex")

	err := home.preflight([]string{"claude", "codex", "claude", "opencode"})
	if err == nil {
		t.Fatal("preflight passed with no login")
	}
	for _, want := range []string{
		"CLAUDE_CONFIG_DIR='" + claude + "' claude auth login",
		"CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY",
		"CODEX_HOME='" + codex + "' codex login",
		`agent "opencode" has no directory in the agent home`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("preflight error lacks %q:\n%v", want, err)
		}
	}
	if strings.Count(err.Error(), "claude auth login") != 1 {
		t.Fatalf("preflight probed claude more than once:\n%v", err)
	}

	writeFile(t, filepath.Join(codex, "auth.json"), "codex login")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "pass-through")
	if err := home.preflight([]string{"claude", "codex"}); err != nil {
		t.Fatalf("preflight with a Codex login and a passed-through Claude token: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(codex, "auth.json")); err != nil || string(got) != "codex login" {
		t.Fatalf("preflight changed the Codex login: %q, %v", got, err)
	}
}
