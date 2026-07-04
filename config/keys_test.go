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

func TestScopeKeyDocsRecursive(t *testing.T) {
	docs, ok := ScopeKeyDocsRecursive(ScopeGlobal)
	if !ok {
		t.Fatal("ScopeGlobal reported unknown")
	}
	got := keySet(docs)
	// Nested tables must flatten into dotted keys.
	for _, k := range []string{
		"worktree.commands.key",
		"workbenches.windows.layout.command",
		"effort.<name>.heavy.model",
		"repo.<name>.trunk",
	} {
		if !got[k] {
			t.Errorf("recursive global missing dotted key %q", k)
		}
	}
	// The pane layout is self-referential (panes contains panes); the cycle
	// guard must list it once and never re-descend into it.
	if got["workbenches.windows.layout.panes.panes"] {
		t.Error("cycle guard failed: layout.panes re-descended into itself")
	}
	if !got["workbenches.windows.layout.panes"] {
		t.Error("recursive global missing workbenches.windows.layout.panes")
	}
	// Every reflected key must carry a description (regression guard for a new
	// field added without a desc tag).
	for _, d := range docs {
		if d.Desc == "" {
			t.Errorf("recursive global key %q has no desc tag", d.Key)
		}
	}
}

func TestTableKeyDocs(t *testing.T) {
	// A real table drills into its children.
	docs, found, isTable, _ := TableKeyDocs(ScopeGlobal, "worktree", false)
	if !found || !isTable {
		t.Fatalf("worktree: found=%v isTable=%v, want both true", found, isTable)
	}
	if !keySet(docs)["commands"] {
		t.Error("worktree table missing 'commands' child")
	}
	// Non-recursive stops at the immediate children.
	if keySet(docs)["commands.key"] {
		t.Error("non-recursive drill should not include grandchild commands.key")
	}
	// Recursive drills all the way down.
	rdocs, _, _, _ := TableKeyDocs(ScopeGlobal, "worktree", true)
	if !keySet(rdocs)["commands.key"] {
		t.Error("recursive drill missing grandchild commands.key")
	}
}

func TestTableKeyDocsUnknown(t *testing.T) {
	if _, found, _, _ := TableKeyDocs(ScopeGlobal, "boguskey", false); found {
		t.Error("unknown key reported as found")
	}
}

func TestTableKeyDocsScalar(t *testing.T) {
	// A legal but scalar key is found yet not a table (no sub-keys).
	docs, found, isTable, leafType := TableKeyDocs(ScopeGlobal, "quick_access_modifier", false)
	if !found {
		t.Error("scalar key should be found")
	}
	if isTable {
		t.Error("scalar key must not be reported as a table")
	}
	if docs != nil {
		t.Errorf("scalar key should yield no docs, got %v", docs)
	}
	if leafType != "string" {
		t.Errorf("scalar leafType: got %q, want %q", leafType, "string")
	}
}

func TestTableKeyDocsMapSegment(t *testing.T) {
	// A map-typed table injects a <name> path segment for its arbitrary key.
	docs, _, isTable, _ := TableKeyDocs(ScopeGlobal, "effort", true)
	if !isTable {
		t.Fatal("effort should be a table")
	}
	if !keySet(docs)["<name>.heavy.model"] {
		t.Errorf("map drill missing <name>.heavy.model; got %v", keySet(docs))
	}
}

func TestTableKeyDocsDottedPath(t *testing.T) {
	// A dotted path walks multiple levels: [repo] block → its workbenches.
	docs, found, isTable, _ := TableKeyDocs(ScopeGlobal, "repo.workbenches", false)
	if !found || !isTable {
		t.Fatalf("repo.workbenches: found=%v isTable=%v, want both true", found, isTable)
	}
	got := keySet(docs)
	// Descended past the [repo] map, so children are Workbench fields, bare.
	for _, k := range []string{"name", "before_apply", "windows"} {
		if !got[k] {
			t.Errorf("repo.workbenches missing child %q; got %v", k, got)
		}
	}

	// A literal <name> map placeholder in the path is tolerated and skipped.
	d2, found2, isTable2, _ := TableKeyDocs(ScopeGlobal, "repo.<name>.workbenches", false)
	if !found2 || !isTable2 {
		t.Fatalf("repo.<name>.workbenches: found=%v isTable=%v, want both true", found2, isTable2)
	}
	if !keySet(d2)["windows"] {
		t.Error("repo.<name>.workbenches missing 'windows' child")
	}

	// Drilling into an effort tier reaches the model/reasoning leaf fields.
	d3, _, isTable3, _ := TableKeyDocs(ScopeGlobal, "effort.heavy", false)
	if !isTable3 {
		t.Fatal("effort.heavy should be a table")
	}
	if !keySet(d3)["model"] || !keySet(d3)["reasoning"] {
		t.Errorf("effort.heavy missing model/reasoning; got %v", keySet(d3))
	}
}

func TestTableKeyDocsDescendThroughScalarFails(t *testing.T) {
	// A path that tries to descend through a scalar segment is not found.
	if _, found, _, _ := TableKeyDocs(ScopeGlobal, "quick_access_modifier.nope", false); found {
		t.Error("descending through a scalar should not resolve")
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
