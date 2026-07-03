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

// ConfigKeyDoc documents a single top-level config key.
type ConfigKeyDoc struct {
	Key  string // TOML key name
	Type string // human-readable TOML type
	Desc string // one-line description from the `desc` struct tag ("" if none)
}

// ScopeKeyDocs returns the top-level keys legal in the given scope, reflected
// from the backing struct's toml/desc tags — the same single source of truth as
// repoScopeLegalKeys (ADR-0083). Embedded structs (the shared RepoScopeConfig)
// are flattened, and fields tagged toml:"-" (never decoded, e.g. .pop.toml's
// trunk) are omitted, so the catalog matches exactly what each surface accepts.
// Keys follow struct declaration order. The bool is false for an unknown scope.
func ScopeKeyDocs(scope ConfigScope) ([]ConfigKeyDoc, bool) {
	var t reflect.Type
	switch scope {
	case ScopeGlobal:
		t = reflect.TypeOf(Config{})
	case ScopeRepo:
		t = reflect.TypeOf(RepoOverrideConfig{})
	case ScopePopTOML:
		t = reflect.TypeOf(RepoConfig{})
	default:
		return nil, false
	}
	return structKeyDocs(t), true
}

// structKeyDocs reflects one struct's toml-tagged fields into ConfigKeyDocs,
// flattening anonymous embedded structs and skipping the toml:"-" sentinel.
func structKeyDocs(t reflect.Type) []ConfigKeyDoc {
	var docs []ConfigKeyDoc
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			docs = append(docs, structKeyDocs(f.Type)...)
			continue
		}
		name := strings.Split(f.Tag.Get("toml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		docs = append(docs, ConfigKeyDoc{
			Key:  name,
			Type: tomlTypeName(f.Type),
			Desc: f.Tag.Get("desc"),
		})
	}
	return docs
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
		elem := t.Elem()
		for elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct {
			return "array of tables"
		}
		return "array"
	case reflect.Map, reflect.Struct:
		return "table"
	default:
		return t.Kind().String()
	}
}
