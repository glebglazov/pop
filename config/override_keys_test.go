package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestOverrideKeysAreTheFourWorkAgentLists pins ADR-0202 decision 4: the cut
// exposes the ordered agent list of each Work group and nothing else, all at
// global scope, each carrying the desc the catalog prints.
func TestOverrideKeysAreTheFourWorkAgentLists(t *testing.T) {
	t.Parallel()
	want := []string{
		"work.implement.agents",
		"work.verify.agents",
		"work.routine.agents",
		"work.attended.agents",
	}
	keys := OverrideKeys()
	var got []string
	for _, k := range keys {
		got = append(got, k.Key)
		if k.Scope != OverrideScopeGlobal {
			t.Errorf("key %s: scope = %q, want %q", k.Key, k.Scope, OverrideScopeGlobal)
		}
		if k.Desc == "" {
			t.Errorf("key %s: registry carries no desc; the editor's rows have nothing to label them with", k.Key)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("override-exposed keys = %v, want %v", got, want)
	}
	for _, key := range want {
		scope, ok := OverrideKeyScope(key)
		if !ok || scope != OverrideScopeGlobal {
			t.Errorf("OverrideKeyScope(%q) = %q, %v; want %q, true", key, scope, ok, OverrideScopeGlobal)
		}
	}
	if _, ok := OverrideKeyScope("work.verify.effort"); ok {
		t.Error("an untagged key reports as override-exposed")
	}
}

// TestOverrideRegistryComesFromTheTags proves there is no hand-maintained list:
// an independent reflection walk of the schema, looking only at `override`
// struct tags, reproduces the registry exactly. Add a tag and the registry
// grows; a list kept beside the tags would fail here.
func TestOverrideRegistryComesFromTheTags(t *testing.T) {
	t.Parallel()
	tagged := overrideTaggedPaths(t, "", reflect.TypeOf(Config{}), map[reflect.Type]bool{})
	var got []string
	for _, k := range OverrideKeys() {
		got = append(got, k.Key+"="+string(k.Scope))
	}
	if strings.Join(got, ",") != strings.Join(tagged, ",") {
		t.Errorf("registry = %v, but the tags say %v", got, tagged)
	}
}

// TestUnknownOverrideScopeFailsLoudly pins that a typo'd tag is refused rather
// than read as "this key is not overridable" — the silent failure the marker
// exists to prevent.
func TestUnknownOverrideScopeFailsLoudly(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"globl", "Global", "machine", " global"} {
		_, err := overrideKeysFrom([]ConfigKeyDoc{{Key: "work.implement.agents", Override: raw}})
		if err == nil {
			t.Errorf("scope %q accepted; want a refusal", raw)
			continue
		}
		if !strings.Contains(err.Error(), raw) || !strings.Contains(err.Error(), "work.implement.agents") {
			t.Errorf("scope %q: error %q names neither the bad word nor the key", raw, err)
		}
	}
	if _, err := overrideKeysFrom([]ConfigKeyDoc{{Key: "work.implement.agents", Override: "repo"}}); err != nil {
		t.Errorf("reserved repo scope refused: %v", err)
	}
}

// overrideTaggedPaths walks the schema for `override` tags, building each
// field's dotted path the way the config catalog addresses it. It is a second
// implementation on purpose: the registry is only proven tag-derived if
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
