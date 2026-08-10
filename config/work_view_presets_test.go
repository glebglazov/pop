package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShippedWorkViewPresetsVocabulary(t *testing.T) {
	presets := ShippedWorkViewPresets()
	wantNames := []string{"active", "unfolded", "recent-7d", "recent-30d", "all", "muted"}
	if len(presets) != len(wantNames) {
		t.Fatalf("shipped count = %d, want %d", len(presets), len(wantNames))
	}
	for i, want := range wantNames {
		if presets[i].Name != want {
			t.Errorf("shipped[%d].Name = %q, want %q", i, presets[i].Name, want)
		}
		if presets[i].System != "" {
			t.Errorf("shipped[%d] must not be a system ref", i)
		}
	}

	active := presets[0]
	if active.Hide == nil || len(active.Hide.Status) != 1 || active.Hide.Status[0] != "done" {
		t.Fatalf("active hide status = %#v, want [done]", active.Hide)
	}
	if active.Hide.Unfolded == nil || *active.Hide.Unfolded {
		t.Fatalf("active hide unfolded = %#v, want false", active.Hide.Unfolded)
	}

	unfolded := presets[1]
	if unfolded.Unfolded == nil || !*unfolded.Unfolded {
		t.Fatalf("unfolded preset unfolded = %#v, want true", unfolded.Unfolded)
	}

	if presets[2].CreatedWithin != "168h" || presets[3].CreatedWithin != "720h" {
		t.Fatalf("recency windows = %q / %q", presets[2].CreatedWithin, presets[3].CreatedWithin)
	}
	if presets[2].Sort != PresetSortCreatedDesc || presets[3].Sort != PresetSortCreatedDesc {
		t.Fatalf("recency sorts = %q / %q, want created_desc", presets[2].Sort, presets[3].Sort)
	}
	if presets[4].Archived != ArchivedInclude {
		t.Fatalf("all archived = %q, want include", presets[4].Archived)
	}

	// The two halves of ADR-0200 decision 8: the view the human sits in loses
	// muted rows, and one preset gets them all back. The muted preset's sort is
	// pinned here because it is a secrecy rule, not a taste: ordering muted rows
	// by resurfacing instant would leak the secret window through position.
	if active.Muted == nil || *active.Muted {
		t.Fatalf("active muted = %#v, want false", active.Muted)
	}
	muted := presets[5]
	if muted.Muted == nil || !*muted.Muted {
		t.Fatalf("muted preset muted = %#v, want true", muted.Muted)
	}
	if muted.Archived != ArchivedInclude {
		t.Fatalf("muted archived = %q, want include", muted.Archived)
	}
	if muted.Sort != PresetSortCreatedDesc {
		t.Fatalf("muted sort = %q, want created_desc (never a resurfacing sort)", muted.Sort)
	}
}

func TestResolveWorkViewPresetsDefaultShipped(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ResolveWorkViewPresets()
	want := ShippedWorkViewPresets()
	if len(got) != len(want) {
		t.Fatalf("resolved count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Errorf("resolved[%d] = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].Number != i+1 {
			t.Errorf("resolved[%d].Number = %d, want %d", i, got[i].Number, i+1)
		}
	}
	if def := cfg.DefaultWorkViewPreset(); def.Name != "active" || def.Number != 1 {
		t.Fatalf("default = %+v, want active #1", def)
	}
}

func TestResolveWorkViewPresetsUserListReplacesShipped(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[[work.dashboard.tasks.presets]]
name = "mine"
status = ["ready"]
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ResolveWorkViewPresets()
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("resolved = %#v, want sole user preset", got)
	}
	for _, p := range got {
		if p.Name == "active" {
			t.Fatal("shipped active must not remain after a user list")
		}
	}
}

func TestResolveWorkViewPresetsSystemReference(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[[work.dashboard.tasks.presets]]
name = "front"
status = ["ready"]

[[work.dashboard.tasks.presets]]
system = "unfolded"

[[work.dashboard.tasks.presets]]
name = "back"
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ResolveWorkViewPresets()
	if len(got) != 3 {
		t.Fatalf("resolved count = %d, want 3: %#v", len(got), got)
	}
	if got[0].Name != "front" || got[2].Name != "back" {
		t.Fatalf("custom ends = %#v", got)
	}
	if got[1].Name != "unfolded" || got[1].System != "" {
		t.Fatalf("system ref = %#v, want expanded unfolded", got[1])
	}
	if got[1].Unfolded == nil || !*got[1].Unfolded {
		t.Fatalf("expanded unfolded filter = %#v", got[1])
	}
	if got[1].Number != 2 {
		t.Fatalf("system ref number = %d, want 2", got[1].Number)
	}
}

func TestWorkViewPresetFindingsDegrade(t *testing.T) {
	body := `
[[work.dashboard.tasks.presets]]
name = "ok"
mystery = true
archived = "sometimes"
sort = "alphabetical"
created_within = "soon"

[[work.dashboard.tasks.presets]]
system = "nope"

[[work.dashboard.tasks.presets]]
name = "ok"

[[work.dashboard.tasks.presets]]
name = "p4"
[[work.dashboard.tasks.presets]]
name = "p5"
[[work.dashboard.tasks.presets]]
name = "p6"
[[work.dashboard.tasks.presets]]
name = "p7"
[[work.dashboard.tasks.presets]]
name = "p8"
[[work.dashboard.tasks.presets]]
name = "p9"
[[work.dashboard.tasks.presets]]
name = "p10"
[[work.dashboard.tasks.presets]]
name = "p11"
`
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load must not fail on bad presets: %v", err)
	}
	wantSubstrings := []string{
		`unknown key "mystery"`,
		`archived "sometimes"`,
		`sort "alphabetical"`,
		`created_within "soon"`,
		`system "nope"`,
		`duplicate preset name "ok"`,
		`10th or later`,
	}
	for _, want := range wantSubstrings {
		if !containsSubstring(cfg.Warnings, want) {
			t.Errorf("Warnings missing %q; got: %v", want, cfg.Warnings)
		}
	}
	// Usable entries still resolve: ok (twice), p4–p10 — system nope dropped.
	got := cfg.ResolveWorkViewPresets()
	if len(got) < 9 {
		t.Fatalf("resolved count = %d, want at least the usable entries: %#v", len(got), got)
	}
}

func TestWorkViewPresetsVisibleInEffectiveTOML(t *testing.T) {
	path := writeConfig(t, "")
	out, err := EffectiveTOML(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active", "unfolded", "recent-7d", "recent-30d", "all"} {
		if !strings.Contains(out, `name = "`+name+`"`) {
			t.Errorf("effective TOML missing preset %q, got:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "work.dashboard.tasks") && !strings.Contains(out, "[[work.dashboard.tasks.presets]]") {
		// BurntSushi may emit [work.dashboard.tasks.presets] array form.
		if !strings.Contains(out, "[work.dashboard.tasks]") && !strings.Contains(out, "presets") {
			t.Fatalf("effective TOML missing work.dashboard.tasks presets:\n%s", out)
		}
	}
}

func TestParsePresetDuration(t *testing.T) {
	d, err := parsePresetDuration("7d")
	if err != nil {
		t.Fatal(err)
	}
	if d != 7*24*time.Hour {
		t.Fatalf("7d = %v, want %v", d, 7*24*time.Hour)
	}
	d, err = parsePresetDuration("168h")
	if err != nil {
		t.Fatal(err)
	}
	if d != 168*time.Hour {
		t.Fatalf("168h = %v", d)
	}
}

func TestWorkViewPresetEmptyUserListReplaces(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[work.dashboard.tasks]
presets = []
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ResolveWorkViewPresets(); len(got) != 0 {
		t.Fatalf("empty declared list should replace shipped, got %#v", got)
	}
}

func TestWorkViewPresetIncludeReplace(t *testing.T) {
	tmp := t.TempDir()
	includePath := filepath.Join(tmp, "extra.toml")
	if err := os.WriteFile(includePath, []byte(`
[[work.dashboard.tasks.presets]]
name = "from-include"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(writeConfig(t, `
includes = ["`+includePath+`"]

[[work.dashboard.tasks.presets]]
name = "from-parent"
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ResolveWorkViewPresets()
	if len(got) != 1 || got[0].Name != "from-parent" {
		t.Fatalf("parent list must replace (not append) include, got %#v", got)
	}
}
