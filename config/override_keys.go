package config

import (
	"fmt"
	"strings"
)

// This file is the registry of overridable config keys (ADR-0212 decision 4).
// Overridability is the default and opt-out: every leaf of a config surface is a
// key a human may override, and reflection over the schema is the whole registry
// — there is no list to keep in step. A field opts out with an
// `override:"never"` struct tag, which it earns only by selecting *where config
// comes from* instead of holding a value.
//
// Two things are never registry entries whatever they are tagged. A table is a
// container, not a unit: the unit is one key's whole value (ADR-0202 decision 2),
// and overriding a table wholesale would silently drop every sub-key the human
// did not retype. And a field of an array-of-tables element, or of a map's
// arbitrarily-named block, is no addressable key at all — `agents.display_name`
// belongs to an entry, not to the config surface.

// overrideNever is the one legal `override` tag value: the marker a field wears
// to opt out of overridability. Any other word is a typo, and a typo read as
// "not overridable" is exactly the silent failure the marker exists to prevent,
// so it is refused loudly.
const overrideNever = "never"

// mapKeySegment is the placeholder the key catalog prints where a map's blocks
// are named by the human ([effort.<agent>]). A path carrying it names no
// concrete key, so no override can be filed against it.
const mapKeySegment = "<name>"

// OverrideKey is one overridable key: its dotted path in the global surface and
// the `desc` text the catalog prints. Desc travels with the key because the
// override editor's row list is built from this registry and must label each row
// without re-reflecting the schema.
type OverrideKey struct {
	Key  string
	Desc string
}

// overrideKeyRegistry is the reflected registry, built once at init from the
// config schema. Building at init rather than lazily makes a mistyped marker
// fail on the first pop command run in the tree, instead of only when whatever
// reads the registry is first opened.
var overrideKeyRegistry = mustOverrideKeys()

// OverrideKeys returns every overridable key in schema declaration order.
func OverrideKeys() []OverrideKey {
	return append([]OverrideKey(nil), overrideKeyRegistry...)
}

// IsOverridableKey reports whether a dotted key path is one a human may override.
// It is how a caller holding a key path asks "may a human override this?"
// without walking the list itself — and it is the gate the write side gives its
// refusals from.
func IsOverridableKey(key string) bool {
	for _, ok := range overrideKeyRegistry {
		if ok.Key == key {
			return true
		}
	}
	return false
}

// OverrideExclusion reports that this catalog row declares itself outside the
// override layer, and the word the catalog marks it with. A renderer asks the
// row rather than the registry because a drilled listing prints keys relative to
// the table it drilled into, so its key paths do not match the registry's
// absolute ones. The tag is already known good: an unrecognised word stopped the
// process at init.
func (d ConfigKeyDoc) OverrideExclusion() (string, bool) {
	if d.Override == "" {
		return "", false
	}
	return d.Override, true
}

// mustOverrideKeys builds the registry, panicking on an unrecognised marker
// anywhere in the schema. The tags are compile-time constants, so the only way
// to reach the panic is a mistyped tag — a build-time mistake that must not
// degrade into a key the editor quietly omits, or a key it quietly offers.
//
// Every surface is validated, so a typo is caught wherever it is written; the
// returned registry holds the global surface's keys, the surface the editor
// addresses by dotted path.
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

// overrideKeysFrom filters a reflected key catalog down to its overridable
// leaves, validating each marker it meets. It takes the docs rather than a
// reflect.Type so the registry reuses the one walk that already backs `pop config
// keys` — and so a test can feed it a catalog carrying a deliberately mistyped
// tag.
//
// The catalog lists a table before the keys inside it, so one pass suffices: a
// node that closes its subtree — an opted-out key, or an array-of-tables whose
// children are element fields — is recorded as a closed prefix, and everything
// arriving under it is skipped.
func overrideKeysFrom(docs []ConfigKeyDoc) ([]OverrideKey, error) {
	var keys []OverrideKey
	var closed []string
	for _, doc := range docs {
		if doc.Override != "" && doc.Override != overrideNever {
			return nil, fmt.Errorf("key %s: unknown override marker %q; the only legal value is %q", doc.Key, doc.Override, overrideNever)
		}
		if underClosedPrefix(doc.Key, closed) {
			continue
		}
		if doc.Override == overrideNever {
			closed = append(closed, doc.Key+".")
			continue
		}
		switch doc.Type {
		case "table":
			// A container: its own leaves are the units, so the walk goes on into
			// them without listing the table itself.
			continue
		case "array of tables":
			// The array is one value and one unit; the fields of its entries are not
			// keys of the surface, so nothing under it is listed.
			closed = append(closed, doc.Key+".")
		}
		if strings.Contains(doc.Key, mapKeySegment) {
			continue
		}
		keys = append(keys, OverrideKey{Key: doc.Key, Desc: doc.Desc})
	}
	return keys, nil
}

// underClosedPrefix reports whether a dotted key sits beneath a node that has
// already answered for its whole subtree.
func underClosedPrefix(key string, closed []string) bool {
	for _, prefix := range closed {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
