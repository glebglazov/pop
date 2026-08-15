package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestConfigKeysMarksTheExclusions pins ADR-0212 decision 4's legibility claim.
// Overridability inverted, so the catalog's mark did too: a human reading it
// sees the few keys they may *not* override from pop, and every unmarked leaf is
// one they may. The marked rows are exactly the keys the registry omits.
func TestConfigKeysMarksTheExclusions(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderScopeKeys(&out, []config.ConfigScope{config.ScopeGlobal}, true, false)
	got := out.String()

	registry := map[string]bool{}
	for _, k := range config.OverrideKeys() {
		registry[k.Key] = true
	}
	if len(registry) == 0 {
		t.Fatal("override registry is empty; every leaf should be in it")
	}
	var marked []string
	for _, line := range strings.Split(got, "\n") {
		key, _, found := strings.Cut(strings.TrimSpace(line), " [override: never]")
		if !found {
			continue
		}
		marked = append(marked, key)
		if registry[key] {
			t.Errorf("catalog marks %q not overridable, but the registry lists it", key)
		}
	}
	if strings.Join(marked, ",") != "includes,repo" {
		t.Errorf("marked rows = %v, want the two keys that select where config comes from", marked)
	}
}

// TestConfigKeysOverridableKeyRendersUnchanged keeps the mark off every key a
// human may override — which is now every leaf, so a whole table of rows carries
// no mark at all.
func TestConfigKeysOverridableKeyRendersUnchanged(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := renderTableKeys(&out, config.ScopeGlobal, "work.verify", false, false); err != nil {
		t.Fatalf("renderTableKeys: %v", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "[override:") {
			t.Errorf("an overridable row carries an override mark: %q", line)
		}
	}
}

// TestConfigKeysHelpExplainsOverrideMark pins that the mark is documented where
// a human meets it — the command's own long help.
func TestConfigKeysHelpExplainsOverrideMark(t *testing.T) {
	t.Parallel()
	help := configKeysCmd.Long
	for _, want := range []string{"[override: never]", "override tag", "Every leaf key"} {
		if !strings.Contains(help, want) {
			t.Errorf("config keys help does not explain the override mark (%q missing):\n%s", want, help)
		}
	}
}
