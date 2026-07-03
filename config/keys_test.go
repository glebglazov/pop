package config

import (
	"testing"
)

// keySet collapses a doc slice to its key names for set comparisons.
func keySet(docs []ConfigKeyDoc) map[string]bool {
	s := make(map[string]bool, len(docs))
	for _, d := range docs {
		s[d.Key] = true
	}
	return s
}

func TestScopeKeyDocsUnknownScope(t *testing.T) {
	if docs, ok := ScopeKeyDocs("nope"); ok || docs != nil {
		t.Fatalf("unknown scope: got (%v, %v), want (nil, false)", docs, ok)
	}
}

func TestScopeKeyDocsPopTOML(t *testing.T) {
	docs, ok := ScopeKeyDocs(ScopePopTOML)
	if !ok {
		t.Fatal("ScopePopTOML reported unknown")
	}
	got := keySet(docs)
	// .pop.toml accepts only the shared repo-scope keys — trunk is toml:"-" here
	// and must never appear, and no global-only key may leak in.
	want := map[string]bool{"workbenches": true, "preferred_workbench": true}
	if len(got) != len(want) {
		t.Fatalf("pop-toml keys: got %v, want exactly %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("pop-toml missing key %q", k)
		}
	}
	if got["trunk"] {
		t.Error("pop-toml must not expose trunk (it is [repo]-only)")
	}
}

func TestScopeKeyDocsRepoBlock(t *testing.T) {
	docs, ok := ScopeKeyDocs(ScopeRepo)
	if !ok {
		t.Fatal("ScopeRepo reported unknown")
	}
	got := keySet(docs)
	for _, k := range []string{"workbenches", "preferred_workbench", "trunk"} {
		if !got[k] {
			t.Errorf("repo block missing key %q", k)
		}
	}
}

func TestScopeKeyDocsGlobal(t *testing.T) {
	docs, ok := ScopeKeyDocs(ScopeGlobal)
	if !ok {
		t.Fatal("ScopeGlobal reported unknown")
	}
	got := keySet(docs)
	for _, k := range []string{"projects", "workbenches", "repo", "queue"} {
		if !got[k] {
			t.Errorf("global missing key %q", k)
		}
	}
	// trunk lives only inside a [repo."<path>"] block, never at global top level.
	if got["trunk"] {
		t.Error("global scope must not expose trunk at top level")
	}
}

// TestScopeKeyDocsMatchesLegalKeys guards against the catalog and the
// scope-legality validator drifting apart: the pop-toml catalog keys must equal
// exactly repoScopeLegalKeys (both reflect RepoScopeConfig, ADR-0083).
func TestScopeKeyDocsMatchesLegalKeys(t *testing.T) {
	docs, _ := ScopeKeyDocs(ScopePopTOML)
	got := keySet(docs)
	legal := repoScopeLegalKeys()
	if len(got) != len(legal) {
		t.Fatalf("catalog/legal-key drift: catalog=%v legal=%v", got, legal)
	}
	for k := range legal {
		if !got[k] {
			t.Errorf("legal key %q missing from pop-toml catalog", k)
		}
	}
}

// TestScopeKeyDocsHaveDescriptions asserts the shared repo-scope keys carry a
// desc tag so `pop config keys` never prints a bare "-" for them.
func TestScopeKeyDocsHaveDescriptions(t *testing.T) {
	docs, _ := ScopeKeyDocs(ScopeRepo)
	for _, d := range docs {
		if d.Desc == "" {
			t.Errorf("repo-scope key %q has no desc tag", d.Key)
		}
		if d.Type == "" {
			t.Errorf("repo-scope key %q has no type", d.Key)
		}
	}
}

func TestTomlTypeName(t *testing.T) {
	docs, _ := ScopeKeyDocs(ScopeRepo)
	byKey := make(map[string]string)
	for _, d := range docs {
		byKey[d.Key] = d.Type
	}
	cases := map[string]string{
		"preferred_workbench": "string",          // string field
		"trunk":               "boolean",         // *bool field
		"workbenches":         "array of tables", // []Workbench field
	}
	for key, want := range cases {
		if got := byKey[key]; got != want {
			t.Errorf("type for %q: got %q, want %q", key, got, want)
		}
	}
}
