package config

import (
	"reflect"
	"strings"
)

// ConfigScope identifies one of pop's config surfaces for key introspection.
// The three scopes back the `pop config keys` command; each maps to the Go
// struct that decodes that surface, so the legal-key set is always reflected
// from the code and can never drift from what actually loads.
type ConfigScope string

const (
	// ScopeGlobal is the user's central config.toml (~/.config/pop/config.toml).
	ScopeGlobal ConfigScope = "global"
	// ScopeRepo is a [repo."<path>"] override block in the global config.toml.
	ScopeRepo ConfigScope = "repo"
	// ScopePopTOML is the committed repo-root .pop.toml (shared, checked in).
	ScopePopTOML ConfigScope = "pop-toml"
)

// ConfigScopes lists the introspectable scopes in display order.
var ConfigScopes = []ConfigScope{ScopeGlobal, ScopePopTOML, ScopeRepo}

// ScopeTitle returns a human-readable one-line label for a scope.
func ScopeTitle(scope ConfigScope) string {
	switch scope {
	case ScopeGlobal:
		return "global config.toml (~/.config/pop/config.toml)"
	case ScopePopTOML:
		return "committed .pop.toml (repo root, shared)"
	case ScopeRepo:
		return `[repo."<path>"] block in global config.toml`
	default:
		return string(scope)
	}
}

// ConfigKeyDoc documents a single config key.
type ConfigKeyDoc struct {
	Key  string // TOML key name (dotted when nested, e.g. "worktree.commands.key")
	Type string // human-readable TOML type
	Desc string // one-line description from the `desc` struct tag ("" if none)
}

// scopeType maps a scope to the Go struct that decodes it.
func scopeType(scope ConfigScope) (reflect.Type, bool) {
	switch scope {
	case ScopeGlobal:
		return reflect.TypeOf(Config{}), true
	case ScopeRepo:
		return reflect.TypeOf(RepoOverrideConfig{}), true
	case ScopePopTOML:
		return reflect.TypeOf(RepoConfig{}), true
	default:
		return nil, false
	}
}

// ScopeKeyDocs returns the top-level keys legal in the given scope, reflected
// from the backing struct's toml/desc tags — the same single source of truth as
// repoScopeLegalKeys (ADR-0083). Embedded structs (the shared RepoScopeConfig)
// are flattened, and fields tagged toml:"-" (never decoded, e.g. .pop.toml's
// trunk) are omitted, so the catalog matches exactly what each surface accepts.
// Keys follow struct declaration order. The bool is false for an unknown scope.
func ScopeKeyDocs(scope ConfigScope) ([]ConfigKeyDoc, bool) {
	t, ok := scopeType(scope)
	if !ok {
		return nil, false
	}
	return structDocs("", t, false, nil), true
}

// ScopeKeyDocsRecursive returns every key legal in the scope, descending into
// nested tables and arrays-of-tables so the caller gets a flat, dotted dump of
// the whole surface. Self-referential tables (a pane layout nesting panes) are
// listed once and not re-descended. The bool is false for an unknown scope.
func ScopeKeyDocsRecursive(scope ConfigScope) ([]ConfigKeyDoc, bool) {
	t, ok := scopeType(scope)
	if !ok {
		return nil, false
	}
	return structDocs("", t, true, map[reflect.Type]bool{}), true
}

// TableKeyDocs returns the keys inside a table of a scope, addressed by a
// dotted path (e.g. "worktree", "repo.workbenches", "effort.heavy"). Each
// segment names a field; the walk descends through nested tables, arrays of
// tables, and maps. A literal "<name>" segment (the map-key placeholder printed
// by --all) is tolerated and skipped, so a path copied from --all output works
// even after the shell eats the angle brackets. recursive descends into deeper
// nested tables at the destination. Return values: docs; found (every segment
// resolved); isTable (the destination decodes a table with sub-keys); leafType
// (the TOML type when the destination is a scalar/array leaf, else "").
func TableKeyDocs(scope ConfigScope, path string, recursive bool) (docs []ConfigKeyDoc, found bool, isTable bool, leafType string) {
	t, ok := scopeType(scope)
	if !ok {
		return nil, false, false, ""
	}
	cur := t
	seg := "" // display-prefix seed: "<name>" when the last hop was through a map
	segments := strings.Split(path, ".")
	for i, name := range segments {
		if name == "<name>" { // map-key placeholder — already descended, skip it
			continue
		}
		f, ok := scopeField(cur, name)
		if !ok {
			return nil, false, false, ""
		}
		elem, s := tableElem(f.Type)
		if elem == nil {
			// Scalar/array leaf: valid only as the final segment, and it has no
			// sub-keys. A non-final scalar means the path tries to descend through it.
			if i == len(segments)-1 {
				return nil, true, false, tomlTypeName(f.Type)
			}
			return nil, false, false, ""
		}
		cur = elem
		seg = s
	}
	// For a map-typed destination the keys live under an arbitrary name
	// ([effort.<agent>]), so seed the prefix with <name> to keep output honest.
	return structDocs(seg, cur, recursive, map[reflect.Type]bool{cur: true}), true, true, ""
}

// scopeField finds the struct field whose top-level toml key is name, searching
// through anonymous embedded structs (the shared RepoScopeConfig). The sentinel
// toml:"-" fields never match, since their tag name is "-", not a real key.
func scopeField(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if sf, ok := scopeField(f.Type, name); ok {
				return sf, true
			}
			continue
		}
		if tomlName(f) == name && name != "" && name != "-" {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// structDocs reflects one struct's toml-tagged fields into ConfigKeyDocs,
// flattening anonymous embedded structs and skipping the toml:"-" sentinel. When
// recursive, it descends into nested table types, prefixing child keys with the
// parent's dotted path and guarding self-referential types via ancestors.
func structDocs(prefix string, t reflect.Type, recursive bool, ancestors map[reflect.Type]bool) []ConfigKeyDoc {
	var docs []ConfigKeyDoc
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			docs = append(docs, structDocs(prefix, f.Type, recursive, ancestors)...)
			continue
		}
		name := tomlName(f)
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		docs = append(docs, ConfigKeyDoc{
			Key:  key,
			Type: tomlTypeName(f.Type),
			Desc: f.Tag.Get("desc"),
		})
		if !recursive {
			continue
		}
		elem, seg := tableElem(f.Type)
		if elem == nil || ancestors[elem] {
			continue
		}
		child := key
		if seg != "" {
			child = key + "." + seg
		}
		ancestors[elem] = true
		docs = append(docs, structDocs(child, elem, recursive, ancestors)...)
		delete(ancestors, elem)
	}
	return docs
}

// tomlName returns a field's top-level toml key (before any options like
// ",omitempty").
func tomlName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("toml"), ",")[0]
}

// tableElem returns the struct type a field descends into for nested-key
// enumeration and the path segment to insert before its children ("" for a
// struct or array-of-tables, "<name>" for a map keyed by an arbitrary name). It
// returns (nil, "") for a scalar or array-of-scalar leaf.
func tableElem(t reflect.Type) (reflect.Type, string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return t, ""
	case reflect.Slice, reflect.Array:
		e := derefElem(t.Elem())
		if e.Kind() == reflect.Struct {
			return e, ""
		}
	case reflect.Map:
		e := derefElem(t.Elem())
		if e.Kind() == reflect.Struct {
			return e, "<name>"
		}
	}
	return nil, ""
}

func derefElem(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// tomlTypeName maps a Go field type to the TOML type a user writes for it.
func tomlTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Slice, reflect.Array:
		if derefElem(t.Elem()).Kind() == reflect.Struct {
			return "array of tables"
		}
		return "array"
	case reflect.Map, reflect.Struct:
		return "table"
	default:
		return t.Kind().String()
	}
}
