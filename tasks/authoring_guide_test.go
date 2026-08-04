package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// enumInGuide pulls the backticked word list that follows prefix out of the
// rendered guide. The comparison below is against *printed text*, not against a
// second call to the generator, so a hand-typed literal creeping into the guide
// body fails here.
func enumInGuide(t *testing.T, guide, prefix string) []string {
	t.Helper()
	idx := strings.Index(guide, prefix)
	if idx < 0 {
		t.Fatalf("guide has no %q line:\n%s", prefix, guide)
	}
	words := regexp.MustCompile("^`[^`]+`(?:\\s*\\|\\s*`[^`]+`)*").
		FindString(strings.TrimSpace(guide[idx+len(prefix):]))
	if words == "" {
		t.Fatalf("no enum after %q", prefix)
	}
	var out []string
	for _, w := range strings.Split(words, "|") {
		out = append(out, strings.Trim(strings.TrimSpace(w), "`"))
	}
	sort.Strings(out)
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestTaskGuideEnumsEqualTheValidatorsSets is the anti-drift guarantee made
// enforced rather than intended (ADR-0183): every enum the guide prints is
// compared to the set validateManifest accepts.
func TestTaskGuideEnumsEqualTheValidatorsSets(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()

	printedTypes := enumInGuide(t, guide, "- `type` — ")
	var validTypes []string
	for tt := range allowedTaskTypes {
		validTypes = append(validTypes, tt)
	}
	if got, want := strings.Join(printedTypes, ","), strings.Join(sortedStrings(validTypes), ","); got != want {
		t.Fatalf("printed types = %q, validator accepts %q", got, want)
	}

	printedStatuses := enumInGuide(t, guide, "- `status` — ")
	var validStatuses []string
	for s := range allowedTaskStatuses {
		validStatuses = append(validStatuses, string(s))
	}
	if got, want := strings.Join(printedStatuses, ","), strings.Join(sortedStrings(validStatuses), ","); got != want {
		t.Fatalf("printed statuses = %q, validator accepts %q", got, want)
	}

	printedEfforts := enumInGuide(t, guide, "- `effort` — ")
	var validEfforts []string
	for e := range allowedTaskEfforts {
		validEfforts = append(validEfforts, e)
	}
	if got, want := strings.Join(printedEfforts, ","), strings.Join(sortedStrings(validEfforts), ","); got != want {
		t.Fatalf("printed efforts = %q, validator accepts %q", got, want)
	}
	for _, tier := range printedEfforts {
		if !IsValidEffort(tier) {
			t.Fatalf("guide prints effort %q the validator rejects", tier)
		}
	}
	if !strings.Contains(guide, "reads as `"+DefaultTaskEffort+"`") {
		t.Fatalf("guide does not name %q as the absent-effort default", DefaultTaskEffort)
	}
}

// TestTaskGuideTemplatesRegisterAsWritten builds a set out of nothing but the
// guide's own output — both task files from the printed template, index.json
// from the printed example — and validates it. A drifted template or a renamed
// manifest field fails here rather than at a session's first register.
func TestTaskGuideTemplatesRegisterAsWritten(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()

	setDir := filepath.Join(t.TempDir(), "2026-08-03-demo")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := fencedBlock(t, guide, "markdown", 0)
	for _, name := range []string{"01-login-form.md", "02-sign-off.md"} {
		if err := os.WriteFile(filepath.Join(setDir, name), []byte(template+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(setDir, ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(fencedBlock(t, guide, "json", 0)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Deps{FS: deps.NewRealFileSystem()}
	m := LoadManifest(d, "2026-08-03-demo", manifestPath)
	if !m.Valid {
		t.Fatalf("the guide's own set does not validate: %v", m.Errors)
	}
	if len(m.Tasks) != 2 {
		t.Fatalf("guide example carries %d tasks, want 2", len(m.Tasks))
	}
	// The example must exercise both types and a blocking edge, or it teaches
	// half the manifest.
	if m.Tasks[0].Type == m.Tasks[1].Type {
		t.Fatalf("guide example uses one type twice (%q)", m.Tasks[0].Type)
	}
	if len(m.Tasks[1].BlockedBy) == 0 {
		t.Fatal("guide example shows no blocking edge")
	}
}

// TestTaskGuideCarriesTheJudgmentRules pins what this guide has that the Map's
// does not: it is the authoritative home of the typing, effort, slicing and
// Orientation rules, so a session authoring a set reads one surface.
func TestTaskGuideCarriesTheJudgmentRules(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()
	for name, want := range map[string]string{
		"prefers AFK":               "Prefer AFK wherever possible",
		"HITL is human work only":   "contains ONLY human work",
		"split the slice":           "**Split the slice.**",
		"approval at the end":       "**Approval at the end.**",
		"setup at the bottom":       "**Setup at the bottom.**",
		"mid-set HITL parks":        "parks the set at `BLOCKED` mid-drain",
		"effort heuristic":          "### Effort is a named signal",
		"effort is not an agent":    "model-strength intent, not an agent choice",
		"vertical slice":            "### A slice is a vertical slice",
		"path-free intent":          "Keep it path-free",
		"orientation rule":          "### Orientation is the one place paths belong",
		"orientation is stale-able": "labelled stale-able",
		"authoritative":             "installed document disagrees with it",
		"drafted but inert":         "only **drafts** the set",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide is missing the %s rule (%q)", name, want)
		}
	}
}

// fencedBlock returns the nth fenced block of the given language from a guide.
func fencedBlock(t *testing.T, guide, lang string, n int) string {
	t.Helper()
	fence := "```" + lang + "\n"
	rest := guide
	for i := 0; ; i++ {
		start := strings.Index(rest, fence)
		if start < 0 {
			t.Fatalf("guide has no %s block #%d", lang, n)
		}
		rest = rest[start+len(fence):]
		end := strings.Index(rest, "```")
		if end < 0 {
			t.Fatalf("unterminated %s block in guide", lang)
		}
		if i == n {
			return strings.TrimRight(rest[:end], "\n")
		}
		rest = rest[end+3:]
	}
}
