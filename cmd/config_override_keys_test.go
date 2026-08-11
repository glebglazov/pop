package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestConfigKeysMarksOverrideExposedKeys pins ADR-0202 decision 3's legibility
// claim: a human reading the catalog sees the overridable keys and the scope
// each override lands at, without opening a TUI. Marked rows agree exactly with
// the reflected registry.
func TestConfigKeysMarksOverrideExposedKeys(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderScopeKeys(&out, []config.ConfigScope{config.ScopeGlobal}, true, false)
	got := out.String()

	registry := config.OverrideKeys()
	if len(registry) == 0 {
		t.Fatal("override registry is empty; nothing for the catalog to mark")
	}
	marked := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		key, rest, found := strings.Cut(strings.TrimSpace(line), " [override: ")
		if !found {
			continue
		}
		scope, _, _ := strings.Cut(rest, "]")
		marked[key+"="+scope] = true
	}
	for _, k := range registry {
		want := k.Key + "=" + string(k.Scope)
		if !marked[want] {
			t.Errorf("global catalog does not mark %q:\n%s", want, got)
		}
		delete(marked, want)
	}
	for extra := range marked {
		t.Errorf("catalog marks %q override-exposed, but the registry does not list it", extra)
	}
}

// TestConfigKeysUntaggedKeyRendersUnchanged keeps the new mark off every key
// that declares no exposure: such a row is listed exactly as before.
func TestConfigKeysUntaggedKeyRendersUnchanged(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := renderTableKeys(&out, config.ScopeGlobal, "work.verify", false, false); err != nil {
		t.Fatalf("renderTableKeys: %v", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "agents") { // the one exposed key in this table
			continue
		}
		if strings.Contains(trimmed, "[override:") {
			t.Errorf("untagged row carries an override mark: %q", line)
		}
	}
}

// TestConfigKeysHelpExplainsOverrideMark pins that the mark is documented where
// a human meets it — the command's own long help.
func TestConfigKeysHelpExplainsOverrideMark(t *testing.T) {
	t.Parallel()
	help := configKeysCmd.Long
	for _, want := range []string{"[override: <scope>]", "override tag"} {
		if !strings.Contains(help, want) {
			t.Errorf("config keys help does not explain the override mark (%q missing):\n%s", want, help)
		}
	}
}
