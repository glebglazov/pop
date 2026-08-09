package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestConfigKeysWithoutWhyKeepsSchemaListing pins ADR-0198 decision 2: without
// --why the catalog stays the reflected schema listing — no reach lines. The
// repo scope's settable marker (decision 6) is a separate always-on annotation
// and is not a reach overlay.
func TestConfigKeysWithoutWhyKeepsSchemaListing(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderScopeKeys(&out, []config.ConfigScope{config.ScopeRepo}, false, false)
	got := out.String()
	if strings.Contains(got, "--max-turns") || strings.Contains(got, "    claude") {
		t.Errorf("without --why, catalog must not layer reach; got:\n%s", got)
	}
	for _, key := range []string{"workbenches", "preferred_workbench", "trunk", "turn_cap"} {
		if !strings.Contains(got, key) {
			t.Errorf("without --why, catalog missing schema key %q:\n%s", key, got)
		}
	}
}

// TestConfigKeysWhyLayersReachFromData pins that --why reads reach through its
// data (ADR-0198's machine-dependence consequence): registered actor/detail
// lines appear, and a key that declares none is listed identically with and
// without the flag.
func TestConfigKeysWhyLayersReachFromData(t *testing.T) {
	const probeKey = "trunk"
	prior, had := config.ConfigKeyReachFor(probeKey)
	t.Cleanup(func() {
		if had {
			config.RegisterConfigKeyReach(probeKey, prior)
		} else {
			config.ClearConfigKeyReach(probeKey)
		}
	})

	want := config.ConfigKeyReach{
		Lines: []config.ConfigKeyReachLine{
			{Actor: "exact-checkout", Detail: "marks this path as the Trunk"},
			{Actor: "other-worktree", Detail: "other worktrees ignore trunk; only the marked path is the fork base"},
		},
	}
	config.RegisterConfigKeyReach(probeKey, want)

	var withWhy, withoutWhy bytes.Buffer
	renderScopeKeys(&withWhy, []config.ConfigScope{config.ScopeRepo}, false, true)
	renderScopeKeys(&withoutWhy, []config.ConfigScope{config.ScopeRepo}, false, false)

	gotWhy := withWhy.String()
	for _, line := range want.Lines {
		if !strings.Contains(gotWhy, line.Actor) || !strings.Contains(gotWhy, line.Detail) {
			t.Errorf("--why output missing reach line %s / %s:\n%s", line.Actor, line.Detail, gotWhy)
		}
	}
	gotPlain := withoutWhy.String()
	if strings.Contains(gotPlain, "exact-checkout") || strings.Contains(gotPlain, "marks this path as the Trunk") {
		t.Errorf("without --why, reach must stay off; got:\n%s", gotPlain)
	}

	config.ClearConfigKeyReach(probeKey)
	var clearedWhy, clearedPlain bytes.Buffer
	renderScopeKeys(&clearedWhy, []config.ConfigScope{config.ScopeRepo}, false, true)
	renderScopeKeys(&clearedPlain, []config.ConfigScope{config.ScopeRepo}, false, false)
	// Compare the probe key's own listing, not the whole catalog — other keys
	// (turn_cap) may still declare reach and legitimately differ under --why.
	whyRow := keyListing(clearedWhy.String(), probeKey)
	plainRow := keyListing(clearedPlain.String(), probeKey)
	if whyRow != plainRow {
		t.Errorf("key with no reach must list identically with and without --why:\n--why: %q\nplain: %q",
			whyRow, plainRow)
	}
}

// TestConfigKeysWhyWorksInAnyCatalogScope proves --why is not repo-only: a
// reach declared for a pop-toml key layers there too.
func TestConfigKeysWhyWorksInAnyCatalogScope(t *testing.T) {
	const probeKey = "preferred_workbench"
	prior, had := config.ConfigKeyReachFor(probeKey)
	t.Cleanup(func() {
		if had {
			config.RegisterConfigKeyReach(probeKey, prior)
		} else {
			config.ClearConfigKeyReach(probeKey)
		}
	})
	config.RegisterConfigKeyReach(probeKey, config.ConfigKeyReach{
		Lines: []config.ConfigKeyReachLine{
			{Actor: "new-session", Detail: "auto-applies the named Workbench"},
		},
	})

	var out bytes.Buffer
	renderScopeKeys(&out, []config.ConfigScope{config.ScopePopTOML}, false, true)
	got := out.String()
	if !strings.Contains(got, "new-session") || !strings.Contains(got, "auto-applies the named Workbench") {
		t.Errorf("pop-toml --why missing reach lines:\n%s", got)
	}
	if strings.Contains(got, repoSettableMarker) {
		t.Errorf("pop-toml must not carry the repo settable marker:\n%s", got)
	}
}

// TestConfigKeysSettableMarkerAgreesWithRepoSetter pins ADR-0198 decision 6 in
// both directions: every RepoSettableKeys entry is marked in the repo catalog,
// and every marked key is in the set the repo setter accepts.
func TestConfigKeysSettableMarkerAgreesWithRepoSetter(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderScopeKeys(&out, []config.ConfigScope{config.ScopeRepo}, false, false)
	got := out.String()

	marked := markedSettableKeys(got)
	settable := config.RepoSettableKeys()
	if len(settable) == 0 {
		t.Fatal("RepoSettableKeys is empty; nothing for the marker to agree with")
	}
	for _, key := range settable {
		if !marked[key] {
			t.Errorf("settable key %q is not marked in the repo catalog:\n%s", key, got)
		}
	}
	for key := range marked {
		found := false
		for _, want := range settable {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("catalog marks %q settable, but RepoSettableKeys does not include it: %v", key, settable)
		}
	}
}

// TestConfigKeysSettableMarkerOnlyInRepoScope keeps the marker on the repo
// surface that decision 6 names — not on global or pop-toml listings of the
// same structural names.
func TestConfigKeysSettableMarkerOnlyInRepoScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []config.ConfigScope{config.ScopeGlobal, config.ScopePopTOML} {
		var out bytes.Buffer
		renderScopeKeys(&out, []config.ConfigScope{scope}, false, false)
		if strings.Contains(out.String(), repoSettableMarker) {
			t.Errorf("%s catalog must not mark keys settable:\n%s", scope, out.String())
		}
	}
}

// TestConfigKeysWhyFlagInHelp verifies --why is registered on pop config keys.
func TestConfigKeysWhyFlagInHelp(t *testing.T) {
	t.Parallel()
	f := configKeysCmd.Flags().Lookup("why")
	if f == nil {
		t.Fatal("expected --why flag registered on config keys")
	}
}

// markedSettableKeys extracts keys that carry repoSettableMarker from a
// rendered catalog. It is the catalog half of the settable ↔ setter agreement.
func markedSettableKeys(catalog string) map[string]bool {
	marked := map[string]bool{}
	for _, line := range strings.Split(catalog, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, repoSettableMarker) {
			continue
		}
		// "turn_cap [settable]  integer  ..." — key is the token before the marker.
		idx := strings.Index(line, repoSettableMarker)
		key := strings.TrimSpace(line[:idx])
		if key != "" {
			marked[key] = true
		}
	}
	return marked
}

// keyListing returns the catalog row for key plus any indented continuation
// lines that belong to it (reach actors under --why).
func keyListing(catalog, key string) string {
	var b strings.Builder
	lines := strings.Split(catalog, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, key+" ") && trimmed != key &&
			!strings.HasPrefix(trimmed, key+repoSettableMarker) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
		for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") {
			i++
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
		break
	}
	return b.String()
}
