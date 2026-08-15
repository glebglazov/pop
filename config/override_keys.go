package config

import (
	"fmt"
	"strings"
)

// This file is the registry of override-exposed config keys (ADR-0202 decision
// 3). A key declares its own exposure with an `override:"<scope>"` struct tag
// naming the scope the override lands at, and reflection over that tag is the
// whole registry — there is no second list to keep in step. The same rule the
// `desc` tag already follows for the key catalog, and the reflected repo-scope
// legal key set (ADR-0083) follows for what a surface accepts.

// OverrideScope names the scope one override lands at.
type OverrideScope string

const (
	// OverrideScopeGlobal files the override once for the machine, in the
	// pop-written config.override.toml. Every exposed key is global today.
	OverrideScopeGlobal OverrideScope = "global"
	// OverrideScopeRepo files the override against one repository, in the same
	// file's [repo."<identity>"] block, where it beats a global entry for the same
	// key (ADR-0212 decisions 2 and 3). The layer honours the scope — the block
	// holds the repo-scope key set and resolution lays it over the ladder — though
	// no key of this registry declares it yet.
	OverrideScopeRepo OverrideScope = "repo"
)

// overrideScopes lists the legal scope words in display order. A tag value
// outside this set is a typo, and a typo that read as "unexposed" is exactly the
// silent failure this marker exists to prevent — so it is refused loudly.
var overrideScopes = []OverrideScope{OverrideScopeGlobal, OverrideScopeRepo}

// OverrideKey is one override-exposed key: its dotted path in the global
// surface, the scope its override lands at, and the `desc` text the catalog
// prints. Desc travels with the key because the override editor's row list is
// built from this registry and must label each row without re-reflecting the
// schema.
type OverrideKey struct {
	Key   string
	Scope OverrideScope
	Desc  string
}

// overrideKeyRegistry is the reflected registry, built once at init from the
// config schema. Building at init rather than lazily makes a mistyped scope word
// fail on the first pop command run in the tree, instead of only when whatever
// reads the registry is first opened.
var overrideKeyRegistry = mustOverrideKeys()

// OverrideKeys returns every override-exposed key in schema declaration order.
func OverrideKeys() []OverrideKey {
	return append([]OverrideKey(nil), overrideKeyRegistry...)
}

// OverrideKeyScope reports the scope a dotted key path is exposed at, and
// whether it is exposed at all. It is how a caller holding a key path asks "may
// a human override this?" without walking the list itself.
func OverrideKeyScope(key string) (OverrideScope, bool) {
	for _, ok := range overrideKeyRegistry {
		if ok.Key == key {
			return ok.Scope, true
		}
	}
	return "", false
}

// OverrideExposure reads the scope this catalog row declares. A renderer asks
// the row rather than the registry because a drilled listing prints keys
// relative to the table it drilled into, so its key paths do not match the
// registry's absolute ones. The tag is already known good: an unrecognised word
// stopped the process at init.
func (d ConfigKeyDoc) OverrideExposure() (OverrideScope, bool) {
	if d.Override == "" {
		return "", false
	}
	scope, err := parseOverrideScope(d.Override)
	if err != nil {
		return "", false
	}
	return scope, true
}

// mustOverrideKeys builds the registry, panicking on an unrecognised scope word
// anywhere in the schema. The tags are compile-time constants, so the only way
// to reach the panic is a mistyped tag — a build-time mistake that must not
// degrade into a key the editor quietly omits.
//
// Every surface is validated, so a typo is caught wherever it is written; the
// returned registry holds the global surface's keys, the only surface an
// override is filed against today.
func mustOverrideKeys() []OverrideKey {
	var global []OverrideKey
	for _, scope := range ConfigScopes {
		docs, ok := ScopeKeyDocsRecursive(scope)
		if !ok {
			panic(fmt.Sprintf("config: scope %s has no backing struct", scope))
		}
		keys, err := overrideKeysFrom(docs)
		if err != nil {
			panic(fmt.Sprintf("config: %s scope: %v", scope, err))
		}
		if scope == ScopeGlobal {
			global = keys
		}
	}
	return global
}

// overrideKeysFrom filters a reflected key catalog down to its exposed keys,
// validating each scope word. It takes the docs rather than a reflect.Type so
// the registry reuses the one walk that already backs `pop config keys` — and so
// a test can feed it a catalog carrying a deliberately mistyped tag.
func overrideKeysFrom(docs []ConfigKeyDoc) ([]OverrideKey, error) {
	var keys []OverrideKey
	for _, doc := range docs {
		if doc.Override == "" {
			continue
		}
		scope, err := parseOverrideScope(doc.Override)
		if err != nil {
			return nil, fmt.Errorf("key %s: %w", doc.Key, err)
		}
		keys = append(keys, OverrideKey{Key: doc.Key, Scope: scope, Desc: doc.Desc})
	}
	return keys, nil
}

// parseOverrideScope reads one `override` tag value.
func parseOverrideScope(raw string) (OverrideScope, error) {
	words := make([]string, 0, len(overrideScopes))
	for _, scope := range overrideScopes {
		if raw == string(scope) {
			return scope, nil
		}
		words = append(words, string(scope))
	}
	return "", fmt.Errorf("unknown override scope %q; want one of: %s", raw, strings.Join(words, ", "))
}
