package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// viewFor picks one key's view out of a resolution over the whole registry, so
// every case exercises the same entry point the dashboard calls.
func viewFor(t *testing.T, f *overrideFixture, key string) OverrideKeyView {
	t.Helper()
	views, err := OverrideKeyViewsWith(f.d, f.userPath)
	if err != nil {
		t.Fatalf("OverrideKeyViewsWith() error: %v", err)
	}
	for _, view := range views {
		if view.Key == key {
			return view
		}
	}
	t.Fatalf("no view for %q in %d views", key, len(views))
	return OverrideKeyView{}
}

// TestOverrideKeyViewsListTheRegistry pins the dashboard's row set to the
// reflected registry: the editor lists every overridable key, in schema order.
func TestOverrideKeyViewsListTheRegistry(t *testing.T) {
	f := newOverrideFixture(t)
	views, err := OverrideKeyViewsWith(f.d, f.userPath)
	if err != nil {
		t.Fatalf("OverrideKeyViewsWith() error: %v", err)
	}
	registry := OverrideKeys()
	if len(views) != len(registry) {
		t.Fatalf("%d views for %d registry keys", len(views), len(registry))
	}
	for i, view := range views {
		if view.Key != registry[i].Key || view.Desc != registry[i].Desc {
			t.Errorf("view %d = %+v, want registry entry %+v", i, view, registry[i])
		}
	}
}

// TestOverrideKeyViewProvenance walks the layers a value can come from. Each
// case is one line of the preview a human reads to answer "why is this the
// value" (ADR-0202 decision 12).
func TestOverrideKeyViewProvenance(t *testing.T) {
	t.Run("hand-authored config.toml", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)

		view := viewFor(t, f, "work.implement.agents")
		if view.Overridden {
			t.Error("Overridden = true with no override file")
		}
		if got := view.Provenance(); got != "config.toml" {
			t.Errorf("Provenance() = %q, want config.toml", got)
		}
		if !strings.Contains(view.EffectiveTOML, `work.implement.agents = [`) ||
			!strings.Contains(view.EffectiveTOML, `"claude"`) {
			t.Errorf("EffectiveTOML = %q, want the hand-authored list as TOML", view.EffectiveTOML)
		}
		if view.SourceTOML != "" || view.SourceProvenance() != "" {
			t.Errorf("source shown without an override: %q / %q", view.SourceTOML, view.SourceProvenance())
		}
	})

	t.Run("override layer, with the source below it", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)
		writeConfigFile(t, f.overridePath, `
[work.implement]
agents = ["codex --model gpt"]
`)

		view := viewFor(t, f, "work.implement.agents")
		if !view.Overridden {
			t.Error("Overridden = false with the key in the override file")
		}
		if got := view.Provenance(); got != "override" {
			t.Errorf("Provenance() = %q, want override", got)
		}
		if !strings.Contains(view.EffectiveTOML, "codex --model gpt") {
			t.Errorf("EffectiveTOML = %q, want the override value", view.EffectiveTOML)
		}
		if !strings.Contains(view.SourceTOML, `"claude"`) {
			t.Errorf("SourceTOML = %q, want the hand-authored value the override stands on", view.SourceTOML)
		}
		if got := view.SourceProvenance(); got != "config.toml" {
			t.Errorf("SourceProvenance() = %q, want config.toml", got)
		}
	})

	t.Run("built-in default when no layer defines the key", func(t *testing.T) {
		f := newOverrideFixture(t)

		view := viewFor(t, f, "work.implement.agents")
		if got := view.Provenance(); got != "built-in default" {
			t.Errorf("Provenance() = %q, want built-in default", got)
		}
		if view.EffectiveTOML != "work.implement.agents = []" {
			t.Errorf("EffectiveTOML = %q, want an empty list rendered as config", view.EffectiveTOML)
		}
	})

	t.Run("include file", func(t *testing.T) {
		f := newOverrideFixture(t)
		include := filepath.Join(filepath.Dir(f.userPath), "extra.toml")
		writeConfigFile(t, include, `
[work.attended]
agents = ["claude"]
`)
		writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]
includes = ["extra.toml"]
`)

		view := viewFor(t, f, "work.attended.agents")
		if got := view.Provenance(); got != "config.toml include ("+include+")" {
			t.Errorf("Provenance() = %q, want the include file named", got)
		}
	})
}

// TestOverrideKeyViewNamesTheTwoEmptyStates is ADR-0202 decision 6's legibility
// claim: "no override" and "overridden to an empty list" both render as an empty
// value, so the preview has to say which in words.
func TestOverrideKeyViewNamesTheTwoEmptyStates(t *testing.T) {
	for _, key := range []string{"work.verify.agents", "work.routine.agents"} {
		t.Run(key+" without an override", func(t *testing.T) {
			f := newOverrideFixture(t)
			writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]

[work.implement]
agents = ["claude"]
`)

			view := viewFor(t, f, key)
			if got := view.Provenance(); got != "fallthrough → work.implement.agents" {
				t.Errorf("Provenance() = %q, want the fallthrough named", got)
			}
			if view.Note != "no override — falls through to the implement list" {
				t.Errorf("Note = %q, want the fallthrough said in words", view.Note)
			}
			if view.Overridden {
				t.Error("Overridden = true with no override file")
			}
		})

		t.Run(key+" overridden to an empty list", func(t *testing.T) {
			f := newOverrideFixture(t)
			writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]

[work.implement]
agents = ["claude"]
`)
			if err := SetOverrideValueWith(f.d, key, []any{}); err != nil {
				t.Fatalf("SetOverrideValueWith() error: %v", err)
			}

			view := viewFor(t, f, key)
			if !view.Overridden {
				t.Fatal("Overridden = false with the key in the override file")
			}
			if got := view.Provenance(); got != "override" {
				t.Errorf("Provenance() = %q, want override", got)
			}
			if view.Note != "override set to an empty list — fallthrough disabled" {
				t.Errorf("Note = %q, want the disabled fallthrough said in words", view.Note)
			}
			if got := view.SourceProvenance(); got != "fallthrough → work.implement.agents" {
				t.Errorf("SourceProvenance() = %q, want what removing the override restores", got)
			}
		})
	}
}

// TestOverrideKeyViewRendersAgentTablesAsConfig keeps the preview in config
// format for the shape an agent list actually takes: a mix of bare commands and
// {display_name, cmd} tables.
func TestOverrideKeyViewRendersAgentTablesAsConfig(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]

[work.attended]
agents = ["claude", { display_name = "Codex", cmd = "codex --model gpt" }]
`)

	view := viewFor(t, f, "work.attended.agents")
	want := `work.attended.agents = [
  "claude",
  { cmd = "codex --model gpt", display_name = "Codex" },
]`
	if view.EffectiveTOML != want {
		t.Errorf("EffectiveTOML =\n%s\nwant\n%s", view.EffectiveTOML, want)
	}
}

// TestOverrideKeyViewsSeeTheRuntimeLayer covers the fourth layer: pop's own
// gap-filler is below the hand-authored file but still a place a value can come
// from, and a preview that omitted it would leave a value with no provenance.
func TestOverrideKeyViewsSeeTheRuntimeLayer(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.runtimePath, `
[work.attended]
agents = ["runtime-agent"]
`)

	view := viewFor(t, f, "work.attended.agents")
	if got := view.Provenance(); got != "config.runtime.toml" {
		t.Errorf("Provenance() = %q, want config.runtime.toml", got)
	}
	if !strings.Contains(view.EffectiveTOML, "runtime-agent") {
		t.Errorf("EffectiveTOML = %q, want the runtime value", view.EffectiveTOML)
	}
}
