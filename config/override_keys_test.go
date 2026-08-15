package config

import (
	"reflect"
	"strings"
	"testing"
)

// globalCatalog indexes the whole global surface by dotted key, so a test can
// ask what type the schema gives a key without re-reflecting it.
func globalCatalog(t *testing.T) map[string]ConfigKeyDoc {
	t.Helper()
	docs, ok := ScopeKeyDocsRecursive(ScopeGlobal)
	if !ok {
		t.Fatal("global scope has no backing struct")
	}
	byKey := make(map[string]ConfigKeyDoc, len(docs))
	for _, doc := range docs {
		byKey[doc.Key] = doc
	}
	return byKey
}

// TestEveryLeafIsOverridable pins ADR-0212 decision 4's inversion: overridability
// is the default, so a leaf that carries no tag at all is in the registry. The
// four agent lists that were the whole registry under the opt-in tag are now
// unremarkable members of it, listed beside leaves of every other shape.
func TestEveryLeafIsOverridable(t *testing.T) {
	t.Parallel()
	registry := map[string]bool{}
	for _, k := range OverrideKeys() {
		registry[k.Key] = true
	}
	untagged := []string{
		"exclude_current_session",               // a bare boolean at the root
		"quick_access_modifier",                 // a bare string at the root
		"projects",                              // an array of tables
		"workbenches",                           // an array of tables with a nested tree
		"work.verify.enabled",                   // a leaf two tables deep
		"work.implement.max_tries",              // a pointer leaf
		"work.implement.git",                    // a pointer to a table — see below
		"worktree.unread_notifications_enabled", // a leaf of another table
		"work.daemon.poll_interval",             // a leaf three tables deep
		"integrations.skills",                   // an array leaf
	}
	for _, key := range untagged {
		if _, ok := globalCatalog(t)[key]; !ok {
			t.Fatalf("premise stale: %q is no longer a key of the global surface", key)
		}
	}
	for _, key := range untagged {
		if globalCatalog(t)[key].Type == "table" {
			continue // tables are covered by their own test, not this one
		}
		if !registry[key] {
			t.Errorf("leaf %q is not overridable, and it carries no tag saying so", key)
		}
	}
	for _, key := range []string{
		"work.implement.agents", "work.verify.agents",
		"work.routine.agents", "work.attended.agents",
	} {
		if !registry[key] {
			t.Errorf("%q dropped out of the registry when the tag inverted", key)
		}
	}
	if len(registry) < 50 {
		t.Errorf("registry holds %d keys; the whole global surface is far larger", len(registry))
	}
	for _, k := range OverrideKeys() {
		if !IsOverridableKey(k.Key) {
			t.Errorf("IsOverridableKey(%q) = false for a registry key", k.Key)
		}
	}
}

// TestOverrideExclusionsAreTheTwoStructuralKeys pins the opt-out half: exactly
// two nodes of the schema wear the marker, both because they select where config
// comes from rather than holding a value — and each takes its whole subtree with
// it, since a key beneath a scope selector is addressed at that scope, not here.
func TestOverrideExclusionsAreTheTwoStructuralKeys(t *testing.T) {
	t.Parallel()
	tagged := overrideTaggedPaths(t, "", reflect.TypeOf(Config{}), map[reflect.Type]bool{})
	want := []string{"includes=never", "repo=never"}
	if strings.Join(tagged, ",") != strings.Join(want, ",") {
		t.Errorf("tagged fields = %v, want %v", tagged, want)
	}
	for _, k := range OverrideKeys() {
		if k.Key == "includes" || k.Key == "repo" || strings.HasPrefix(k.Key, "repo.") {
			t.Errorf("%q is listed as overridable; it selects where config comes from", k.Key)
		}
	}
	if IsOverridableKey("includes") || IsOverridableKey("repo") {
		t.Error("a structural key answers as overridable")
	}
	for _, key := range []string{"includes", "repo"} {
		marker, excluded := globalCatalog(t)[key].OverrideExclusion()
		if !excluded || marker != overrideNever {
			t.Errorf("catalog row %q reports exclusion %q, %v; want %q, true", key, marker, excluded, overrideNever)
		}
	}
	if _, excluded := globalCatalog(t)["work.verify.enabled"].OverrideExclusion(); excluded {
		t.Error("an untagged leaf reports itself excluded")
	}
}

// TestATableIsNeverAnOverrideTarget pins the second half of decision 4: the unit
// is one leaf's whole value, so no row of the registry is a container. Its
// leaves are listed instead — overriding the table would silently drop every
// sub-key the human did not retype.
func TestATableIsNeverAnOverrideTarget(t *testing.T) {
	t.Parallel()
	catalog := globalCatalog(t)
	for _, k := range OverrideKeys() {
		doc, ok := catalog[k.Key]
		if !ok {
			t.Errorf("registry key %q is in no catalog row", k.Key)
			continue
		}
		if doc.Type == "table" {
			t.Errorf("registry lists table %q as an override target", k.Key)
		}
		if strings.Contains(k.Key, mapKeySegment) {
			t.Errorf("registry lists %q, which names no concrete key", k.Key)
		}
	}
	for _, table := range []string{"work", "work.verify", "worktree", "effort", "agents"} {
		if IsOverridableKey(table) {
			t.Errorf("table %q answers as overridable", table)
		}
	}
	if !IsOverridableKey("work.verify.enabled") {
		t.Error("a leaf inside a table is not overridable; only the table was meant to be excluded")
	}
	// An array of tables is one value and one unit: the array is overridable and
	// the fields of its entries are not keys of the surface at all.
	if !IsOverridableKey("workbenches") {
		t.Error("workbenches is not overridable; an array is a value, not a container")
	}
	if IsOverridableKey("workbenches.name") || IsOverridableKey("work.implement.agents.display_name") {
		t.Error("a field of an array entry answers as a key of the surface")
	}
}

// TestUnknownOverrideMarkerFailsLoudly pins that a typo'd tag is refused rather
// than read as "this key is overridable like every other" — the silent failure
// the marker exists to prevent, now that the default is exposure.
func TestUnknownOverrideMarkerFailsLoudly(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"nver", "Never", "global", "repo", " never"} {
		_, err := overrideKeysFrom([]ConfigKeyDoc{{Key: "includes", Type: "array", Override: raw}})
		if err == nil {
			t.Errorf("marker %q accepted; want a refusal", raw)
			continue
		}
		if !strings.Contains(err.Error(), raw) || !strings.Contains(err.Error(), "includes") {
			t.Errorf("marker %q: error %q names neither the bad word nor the key", raw, err)
		}
	}
	if _, err := overrideKeysFrom([]ConfigKeyDoc{{Key: "includes", Type: "array", Override: overrideNever}}); err != nil {
		t.Errorf("the one legal marker refused: %v", err)
	}
}

// TestStructuralKeysAreRefusedAsOverrideTargets follows the exclusion through to
// the write side: the editor's gate reads the same registry, so neither key can
// be stored however the buffer is spelled.
func TestStructuralKeysAreRefusedAsOverrideTargets(t *testing.T) {
	t.Parallel()
	for key, buffer := range map[string]string{
		"includes": `includes = ["/tmp/other.toml"]`,
		"repo":     "[repo.\"/tmp/x\"]\nturn_cap = 4",
	} {
		if _, problem := parseOverrideBuffer(key, buffer); problem == "" {
			t.Errorf("%s accepted as an override target", key)
		} else if !strings.Contains(problem, key) {
			t.Errorf("%s refusal %q does not name the key", key, problem)
		}
	}
	f := newOverrideFixture(t)
	if err := CopyOverrideFromSourceWith(f.d, f.userPath, "includes"); err == nil {
		t.Error("copying the source down stored an override of includes")
	}
}

// overrideTaggedPaths walks the schema for `override` tags, building each
// field's dotted path the way the config catalog addresses it. It is a second
// implementation on purpose: the exclusion set is only proven tag-derived if
// something that shares no code with it agrees.
func overrideTaggedPaths(t *testing.T, prefix string, typ reflect.Type, seen map[reflect.Type]bool) []string {
	t.Helper()
	var paths []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get("toml"), ",")[0]
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			paths = append(paths, overrideTaggedPaths(t, prefix, f.Type, seen)...)
			continue
		}
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		if tag := f.Tag.Get("override"); tag != "" {
			paths = append(paths, key+"="+tag)
		}
		elem := f.Type
		for elem.Kind() == reflect.Ptr || elem.Kind() == reflect.Slice ||
			elem.Kind() == reflect.Array || elem.Kind() == reflect.Map {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct || seen[elem] {
			continue
		}
		child := key
		if f.Type.Kind() == reflect.Map {
			child = key + ".<name>"
		}
		seen[elem] = true
		paths = append(paths, overrideTaggedPaths(t, child, elem, seen)...)
		delete(seen, elem)
	}
	return paths
}
