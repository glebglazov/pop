package config

import "testing"

// A Contested key is one more than one layer states a value for (ADR-0212
// decision 8). It is what the Config dashboard sorts first and marks, so these
// tests are about the detection alone: which layers count, and which do not.

// TestContestedKeysAtGlobalScope walks the global surface: a key two layers
// state, a key one layer states, and a key nobody states.
func TestContestedKeysAtGlobalScope(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)
	writeConfigFile(t, f.overridePath, `
[work.implement]
agents = ["codex --model gpt"]
`)

	if view := viewFor(t, f, "work.implement.agents"); !view.Contested {
		t.Errorf("%s: Contested = false with both the override and config.toml stating it", view.Key)
	}
	// Stated once, over a built-in default. The default is no contender: it
	// states every key of the surface, so counting it would mark the whole list.
	if view := viewFor(t, f, "work.implement.max_tries"); view.Contested {
		t.Errorf("%s: Contested = true with only config.toml stating it", view.Key)
	}
	if view := viewFor(t, f, "worktree.unread_notifications_enabled"); view.Contested {
		t.Errorf("%s: Contested = true with no layer stating it", view.Key)
	}
}

// TestContestedKeysCountAnIncludeAsItsOwnLayer pins that a contest is between
// layers rather than between files pop wrote: an include ranks with the config
// that pulled it in, and the two of them still disagree.
func TestContestedKeysCountAnIncludeAsItsOwnLayer(t *testing.T) {
	f := newOverrideFixture(t)
	include := f.userPath + ".d/agents.toml"
	writeConfigFile(t, include, "[work.implement]\nagents = [\"claude\"]\n")
	writeConfigFile(t, f.userPath, "includes = [\""+include+"\"]\n[work.implement]\nagents = [\"codex\"]\n")

	if view := viewFor(t, f, "work.implement.agents"); !view.Contested {
		t.Errorf("%s: Contested = false with config.toml and its include disagreeing", view.Key)
	}
}

// TestContestedKeysAtRepositoryScope asks the same question of the rows that
// answer for one repository, where the layers are the override's block for it,
// the [repo."<path>"] declaration and the committed .pop/config.toml.
func TestContestedKeysAtRepositoryScope(t *testing.T) {
	f := newOverrideScopeFixture(t)
	f.commit(t, f.main, "committed")
	f.writeOverride(t, `
[repo.`+quoted(f.identity)+`]
preferred_workbench = "stated"
turn_cap = 7
`)

	if view := f.repoView(t, f.main, "repo.preferred_workbench"); !view.Contested {
		t.Errorf("%s: Contested = false with the override and .pop/config.toml disagreeing", view.Key)
	}
	// Stated in the override layer and nowhere else — an override is not a
	// contest on its own.
	if view := f.repoView(t, f.main, "repo.turn_cap"); view.Contested {
		t.Errorf("%s: Contested = true with only the override stating it", view.Key)
	}
}
