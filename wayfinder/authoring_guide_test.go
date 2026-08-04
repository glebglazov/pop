package wayfinder

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
// rendered guide. The tests below compare *printed text* against the validator's
// own sets rather than comparing one generator call to another, so a hand-typed
// literal creeping into the guide body fails here.
func enumInGuide(t *testing.T, guide, prefix string) []string {
	t.Helper()
	idx := strings.Index(guide, prefix)
	if idx < 0 {
		t.Fatalf("guide has no %q line:\n%s", prefix, guide)
	}
	rest := guide[idx+len(prefix):]
	words := regexp.MustCompile("^`[^`]+`(?:\\s*\\|\\s*`[^`]+`)*").FindString(strings.TrimSpace(rest))
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

// TestMapGuideEnumsEqualTheValidatorsSets is the anti-drift guarantee made
// enforced rather than intended (ADR-0183): every enum the guide prints is
// compared to the set pop actually validates against.
func TestMapGuideEnumsEqualTheValidatorsSets(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()

	printedTypes := enumInGuide(t, guide, "- `type` — ")
	var validTypes []string
	for tt := range manifestTicketTypes {
		validTypes = append(validTypes, string(tt))
	}
	if got, want := strings.Join(printedTypes, ","), strings.Join(sortedStrings(validTypes), ","); got != want {
		t.Fatalf("printed ticket types = %q, validator accepts %q", got, want)
	}

	printedStatuses := enumInGuide(t, guide, "- `status` — ")
	var validStatuses []string
	for s := range manifestTicketStatuses {
		validStatuses = append(validStatuses, string(s))
	}
	if got, want := strings.Join(printedStatuses, ","), strings.Join(sortedStrings(validStatuses), ","); got != want {
		t.Fatalf("printed ticket statuses = %q, validator accepts %q", got, want)
	}

	// The Status: vocabulary is what the parser accepts, no more and no less:
	// every printed word parses, and BROKEN — pop's verdict, never a word a
	// session writes — is not offered.
	printedMapStatuses := enumInGuide(t, guide, "The `Status:` line sits above the first heading and takes\n")
	for _, word := range printedMapStatuses {
		if word == string(MapBroken) {
			t.Fatalf("the guide offers %q as an authorable status", word)
		}
		if _, err := parseMapStatus(word); err != nil {
			t.Fatalf("guide prints status %q the parser rejects: %v", word, err)
		}
	}
	var parseable []string
	for _, candidate := range []MapStatus{MapActive, MapArrived, MapAbandoned, MapBroken} {
		if _, err := parseMapStatus(string(candidate)); err == nil {
			parseable = append(parseable, string(candidate))
		}
	}
	if got, want := strings.Join(printedMapStatuses, ","), strings.Join(sortedStrings(parseable), ","); got != want {
		t.Fatalf("printed map statuses = %q, parser accepts %q", got, want)
	}
}

// TestMapGuideTemplatesRegisterAsWritten builds a Map out of nothing but the
// guide's own output — map.md, both ticket files, index.json — and puts it
// through the parser and the manifest validator. A template that has drifted
// from what pop reads back fails here rather than in a charting session.
func TestMapGuideTemplatesRegisterAsWritten(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()

	mapDir := filepath.Join(t.TempDir(), "2026-08-03-demo")
	if err := os.MkdirAll(filepath.Join(mapDir, issuesDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mapDir, rel), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(mapFileName, fencedBlock(t, guide, "markdown", 0))
	write(MapManifestFileName, fencedBlock(t, guide, "json", 0))
	ticket := fencedBlock(t, guide, "markdown", 1)
	write(filepath.Join(issuesDirName, "01-storage-shape.md"), ticket)
	write(filepath.Join(issuesDirName, "02-read-path.md"), ticket)

	d := &Deps{FS: deps.NewRealFileSystem()}
	manifest, err := LoadMapManifest(d, mapDir)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Valid {
		t.Fatalf("the guide's own manifest does not validate: %v", manifest.Errors)
	}

	mapMD, err := os.ReadFile(filepath.Join(mapDir, mapFileName))
	if err != nil {
		t.Fatal(err)
	}
	status, destination, err := ParseMapMarkdown(string(mapMD))
	if err != nil {
		t.Fatalf("the guide's map.md does not parse: %v", err)
	}
	if status != MapActive {
		t.Fatalf("status = %q, want %q", status, MapActive)
	}
	if destination == "" {
		t.Fatal("the guide's map.md has no destination for a session to replace")
	}

	// The markers the guide tells a session not to touch must be the markers the
	// resolve render finds.
	lines := strings.Split(string(mapMD), "\n")
	for _, region := range mapGeneratedRegions {
		if _, _, found := locateGeneratedRegion(lines, region.name); !found {
			t.Fatalf("map.md template carries no %q region pop can locate", region.name)
		}
	}
	if _, _, found := locateGeneratedRegion(strings.Split(ticket, "\n"), answerRegionName); !found {
		t.Fatal("the ticket template carries no answer region pop can locate")
	}
}

// TestMapGuideStatesTheOnePassRule pins the two facts the guide exists to teach
// a session that would otherwise reach for a create verb.
func TestMapGuideStatesTheOnePassRule(t *testing.T) {
	t.Parallel()
	guide := AuthoringGuide()
	for _, want := range []string{
		"You pick the ids, and you write everything in one pass",
		"There is no\ncreate-then-wire second pass",
		"*server* mints issue ids",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide is missing %q", want)
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
