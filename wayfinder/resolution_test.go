package wayfinder

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

const resolveMapID = "2026-08-03-resolving"

// resolveMapMarkdown is a map.md as a charting session leaves it: prose sections
// the session owns, and index sections carrying nothing but a stale hand-written
// line from before pop owned them.
const resolveMapMarkdown = `Status: active

## Destination

Ship the thing.

## Notes

Prose only a session writes.

## Decisions so far

- hand-written line from before pop generated this

## Not yet specified

Fog stays here.

## Out of scope

## Spawned sets
`

func resolveFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := "maps/" + resolveMapID + "/"
	d, storageDir := registryFixture(t, map[string]string{
		dir + "map.md":                 resolveMapMarkdown,
		dir + "issues/01-first.md":     "## Question\n\nWhich database?\n",
		dir + "issues/02-second.md":    "## Question\n\nWhich schema?\n",
		dir + "issues/03-third.md":     "## Question\n\nWhich client?\n",
		dir + "adrs/978d65fd-slug.md":  "# Decision\n\nUse Postgres.\n",
		dir + "adrs/aaaa1111-other.md": "# Decision\n\nUse SQLite.\n",
		dir + "context/09-database.md": "+ Database — the relational store.\n",
		dir + "index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"Database","type":"grilling","status":"open","blocked_by":[]},` +
			`{"id":"02","file":"02-second.md","title":"Schema","type":"grilling","status":"open","blocked_by":["01"]},` +
			`{"id":"03","file":"03-third.md","title":"Client","type":"research","status":"open","blocked_by":[]}` +
			`],"spawned_sets":["2026-08-04-implementing"]}`,
	})
	mustRegister(t, d, resolveMapID)
	asWindow(d, "pane:%1", at(9))
	return d, storageDir
}

func answerFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func manifestEntry(t *testing.T, d *Deps, mapDir, id string) ManifestTicket {
	t.Helper()
	manifest, err := LoadMapManifest(d, mapDir)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Valid {
		t.Fatalf("manifest is invalid after a resolve: %s", manifest.MalformedReason())
	}
	for _, entry := range manifest.Tickets {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("manifest has no ticket %q", id)
	return ManifestTicket{}
}

// TestResolveWritesAnswerManifestAndIndexInOneCall is the whole verb: one call
// leaves the answer on the ticket, the manifest resolved, the index regenerated
// and the claim handed back.
func TestResolveWritesAnswerManifestAndIndexInOneCall(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	if _, err := ClaimTicket(d, "", resolveMapID, "01"); err != nil {
		t.Fatalf("claim 01: %v", err)
	}

	result, err := ResolveTicket(d, "", ResolveRequest{
		MapID:      resolveMapID,
		Ticket:     "01",
		AnswerFile: answerFile(t, "Postgres, because the data is relational.\n\nThe long form lives here.\n"),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Replaced || result.OutOfScope || result.ReleasedClaim != "pane:%1" {
		t.Fatalf("result = %+v, want a first resolution releasing pane:%%1", result)
	}

	ticket := readFile(t, filepath.Join(mapDir, "issues", "01-first.md"))
	for _, want := range []string{"## Question", "Which database?", "## Answer", "Postgres, because the data is relational.", "The long form lives here."} {
		if !strings.Contains(ticket, want) {
			t.Fatalf("ticket markdown missing %q:\n%s", want, ticket)
		}
	}

	if entry := manifestEntry(t, d, mapDir, "01"); entry.Status != TicketResolved || entry.OutOfScope {
		t.Fatalf("manifest entry = %+v, want resolved and in scope", entry)
	}

	mapMD := readFile(t, filepath.Join(mapDir, "map.md"))
	for _, want := range []string{
		"<!-- pop:generated decisions -->",
		"<!-- /pop:generated decisions -->",
		"- [Database](issues/01-first.md) — Postgres, because the data is relational.",
		"<!-- pop:generated out-of-scope -->",
		"<!-- pop:generated spawned-sets -->",
		"- 2026-08-04-implementing",
		"Prose only a session writes.",
		"Fog stays here.",
	} {
		if !strings.Contains(mapMD, want) {
			t.Fatalf("map.md missing %q:\n%s", want, mapMD)
		}
	}
	// The first generated rewrite takes the section over, discards and all.
	if strings.Contains(mapMD, "hand-written line from before") {
		t.Fatalf("a hand-written index line survived the first generated rewrite:\n%s", mapMD)
	}

	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.FindWorkClaim(MapTicketRef(resolveMapID, "01")); err != nil || found {
		t.Fatalf("resolving left the claim in place (found=%v, err=%v)", found, err)
	}
}

// TestResolveIsRerunnableAndReplacesTheAnswer: a wrong answer is fixed by running
// the verb again, never by hand-editing what it wrote.
func TestResolveIsRerunnableAndReplacesTheAnswer(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres, for the joins.\n"),
	}); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	second, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "SQLite, the joins never came.\n"),
	})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if !second.Replaced {
		t.Fatal("re-resolving an already-resolved ticket did not report a replacement")
	}

	ticket := readFile(t, filepath.Join(mapDir, "issues", "01-first.md"))
	if n := strings.Count(ticket, "## Answer"); n != 1 {
		t.Fatalf("ticket carries %d Answer sections, want 1:\n%s", n, ticket)
	}
	if strings.Contains(ticket, "Postgres") {
		t.Fatalf("the replaced answer is still there:\n%s", ticket)
	}

	mapMD := readFile(t, filepath.Join(mapDir, "map.md"))
	if n := strings.Count(mapMD, "issues/01-first.md"); n != 1 {
		t.Fatalf("the index lists ticket 01 %d times, want 1:\n%s", n, mapMD)
	}
	if !strings.Contains(mapMD, "— SQLite, the joins never came.") {
		t.Fatalf("the index still gists the replaced answer:\n%s", mapMD)
	}
}

// headingAnswer is the answer shape that used to break the verb: a research
// write-up whose own `## ` headings sit past the point a section scan stops.
const headingAnswer = `Postgres, because the data is relational.

## Citations

- [the manual](https://example.com/manual)

## Alternatives considered

SQLite, ruled out on the joins.
`

// TestResolveReplacesAnAnswerCarryingHeadings: the answer region is delimited by
// markers, not by heading structure, so a body with its own `## ` headings lands
// once and is replaced whole on the next run rather than accumulating.
func TestResolveReplacesAnAnswerCarryingHeadings(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	ticketPath := filepath.Join(storageDir, "maps", resolveMapID, "issues", "03-third.md")
	mapPath := filepath.Join(storageDir, "maps", resolveMapID, "map.md")

	resolve := func(body string) {
		t.Helper()
		if _, err := ResolveTicket(d, "", ResolveRequest{
			MapID: resolveMapID, Ticket: "03", AnswerFile: answerFile(t, body),
		}); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	}

	resolve(headingAnswer)
	first := readFile(t, ticketPath)
	if got := strings.TrimSpace(regionBody(t, first, "answer")); got != strings.TrimSpace(headingAnswer) {
		t.Fatalf("answer region = %q, want the answer body verbatim", got)
	}
	if n := strings.Count(first, "## Citations"); n != 1 {
		t.Fatalf("ticket carries %d citation headings, want 1:\n%s", n, first)
	}
	// The gist skips the region markers and the headings, as map.md's index needs.
	if want := "— Postgres, because the data is relational."; !strings.Contains(readFile(t, mapPath), want) {
		t.Fatalf("map.md missing %q:\n%s", want, readFile(t, mapPath))
	}

	resolve(headingAnswer)
	if second := readFile(t, ticketPath); second != first {
		t.Fatalf("re-resolving the same answer changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	resolve("Redis, the joins never came.\n")
	third := readFile(t, ticketPath)
	for _, gone := range []string{"Citations", "Alternatives considered", "Postgres", "SQLite"} {
		if strings.Contains(third, gone) {
			t.Fatalf("the replaced answer left %q behind:\n%s", gone, third)
		}
	}
	if !strings.Contains(third, "Which client?") {
		t.Fatalf("replacing the answer ate the question:\n%s", third)
	}
}

// TestResolveFoldsAPreMarkerAnswer: a ticket resolved before the markers existed
// — including one the old write path duplicated — is healed by resolving again.
func TestResolveFoldsAPreMarkerAnswer(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	ticketPath := filepath.Join(storageDir, "maps", resolveMapID, "issues", "03-third.md")
	legacy := `## Question

Which client?

## Answer

The old answer.

## Citations

- [a stale source](https://example.com/stale)

The old answer.

## Citations

- [a stale source](https://example.com/stale)
`
	if err := os.WriteFile(ticketPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "03", AnswerFile: answerFile(t, "The Go client.\n"),
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	ticket := readFile(t, ticketPath)
	if got := strings.TrimSpace(regionBody(t, ticket, "answer")); got != "The Go client." {
		t.Fatalf("answer region = %q, want the new answer alone", got)
	}
	for _, gone := range []string{"The old answer.", "stale"} {
		if strings.Contains(ticket, gone) {
			t.Fatalf("the fold preserved %q:\n%s", gone, ticket)
		}
	}
	if n := strings.Count(ticket, "## Answer"); n != 1 {
		t.Fatalf("ticket carries %d Answer sections, want 1:\n%s", n, ticket)
	}
	if !strings.Contains(ticket, "Which client?") {
		t.Fatalf("the fold ate the question:\n%s", ticket)
	}
}

// TestGeneratedRegionSurvivesAHeadingInItsBody covers map.md's side of the same
// marker lookup: today its bodies are single-line link lists, and a heading in
// one must not truncate the region.
func TestGeneratedRegionSurvivesAHeadingInItsBody(t *testing.T) {
	t.Parallel()
	section := generatedSection{
		generatedRegion: regionDecisions,
		body:            []string{"- [Database](issues/01-first.md) — Postgres.", "", "## A heading inside the region", "", "- [Client](issues/03-third.md) — Go."},
	}
	first := renderGeneratedSections(resolveMapMarkdown, []generatedSection{section})
	if got := strings.TrimSpace(regionBody(t, first, "decisions")); got != strings.TrimSpace(strings.Join(section.body, "\n")) {
		t.Fatalf("region = %q, want the body verbatim", got)
	}

	second := renderGeneratedSections(first, []generatedSection{section})
	if second != first {
		t.Fatalf("re-rendering the region changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	shrunk := renderGeneratedSections(second, []generatedSection{{
		generatedRegion: regionDecisions,
		body:            []string{"- [Database](issues/01-first.md) — Postgres."},
	}})
	if strings.Contains(shrunk, "A heading inside the region") {
		t.Fatalf("a heading in the old body survived the rewrite:\n%s", shrunk)
	}
	for _, want := range []string{"Prose only a session writes.", "Fog stays here.", "## Not yet specified"} {
		if !strings.Contains(shrunk, want) {
			t.Fatalf("map.md lost %q:\n%s", want, shrunk)
		}
	}
}

// TestResolveRefusalTouchesNothing: validation runs before the first byte, so a
// refusal cannot leave a half-resolved ticket behind.
func TestResolveRefusalTouchesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  ResolveRequest
		want string
	}{
		{"no answer file", ResolveRequest{MapID: resolveMapID, Ticket: "01"}, "--answer-file"},
		{"missing answer file", ResolveRequest{MapID: resolveMapID, Ticket: "01", AnswerFile: "/nowhere/answer.md"}, "read answer file"},
		{"unknown ticket", ResolveRequest{MapID: resolveMapID, Ticket: "77", AnswerFile: "keep"}, "no ticket"},
		{"unknown map", ResolveRequest{MapID: "2026-01-01-nope", Ticket: "01", AnswerFile: "keep"}, "unknown wayfinder map"},
		{"missing adr draft", ResolveRequest{MapID: resolveMapID, Ticket: "01", AnswerFile: "keep", ADRDrafts: []string{"adrs/nope.md"}}, "--adr adrs/nope.md"},
		{"missing context draft", ResolveRequest{MapID: resolveMapID, Ticket: "01", AnswerFile: "keep", ContextDrafts: []string{"context/nope.md"}}, "--context context/nope.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, storageDir := resolveFixture(t)
			mapDir := filepath.Join(storageDir, "maps", resolveMapID)
			before := snapshotDir(t, mapDir)

			req := tt.req
			if req.AnswerFile == "keep" {
				req.AnswerFile = answerFile(t, "An answer nobody records.\n")
			}
			_, err := ResolveTicket(d, "", req)
			if err == nil {
				t.Fatal("expected the resolve to refuse")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
			for path, content := range snapshotDir(t, mapDir) {
				if before[path] != content {
					t.Fatalf("a refused resolve wrote to %s:\n%s", path, content)
				}
			}
			if len(before) != len(snapshotDir(t, mapDir)) {
				t.Fatal("a refused resolve added or removed a file")
			}
		})
	}
}

// TestOutOfScopeRendersIntoItsOwnSection pins the reason the two resolutions are
// two verbs: a scope boundary never appears on the route walked.
func TestOutOfScopeRendersIntoItsOwnSection(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres.\n"),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := RuleOutOfScope(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "03", Reason: "The client is a separate effort.",
	})
	if err != nil {
		t.Fatalf("out-of-scope: %v", err)
	}
	if !result.OutOfScope {
		t.Fatalf("result = %+v, want an out-of-scope resolution", result)
	}

	if entry := manifestEntry(t, d, mapDir, "03"); entry.Status != TicketResolved || !entry.OutOfScope {
		t.Fatalf("manifest entry = %+v, want resolved and out of scope", entry)
	}
	if ticket := readFile(t, filepath.Join(mapDir, "issues", "03-third.md")); !strings.Contains(ticket, "The client is a separate effort.") {
		t.Fatalf("the reason was not written to the ticket:\n%s", ticket)
	}

	mapMD := readFile(t, filepath.Join(mapDir, "map.md"))
	decisions := regionBody(t, mapMD, "decisions")
	scope := regionBody(t, mapMD, "out-of-scope")
	if strings.Contains(decisions, "03-third.md") {
		t.Fatalf("an out-of-scope ticket landed in the decision index:\n%s", decisions)
	}
	if !strings.Contains(scope, "- [Client](issues/03-third.md) — The client is a separate effort.") {
		t.Fatalf("Out of scope region = %q", scope)
	}
	if !strings.Contains(decisions, "01-first.md") {
		t.Fatalf("the decision index lost its entry:\n%s", decisions)
	}

	if _, err := RuleOutOfScope(d, "", ResolveRequest{MapID: resolveMapID, Ticket: "03"}); err == nil ||
		!strings.Contains(err.Error(), "--reason") {
		t.Fatalf("out-of-scope without a reason = %v", err)
	}
}

// TestGeneratedRegionsAreRebuiltAndProseSurvives: pop owns what sits between the
// markers and nothing else in the file.
func TestGeneratedRegionsAreRebuiltAndProseSurvives(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)
	mapPath := filepath.Join(mapDir, "map.md")

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres.\n"),
	}); err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(readFile(t, mapPath),
		"<!-- pop:generated decisions -->",
		"A session's note above the region.\n\n<!-- pop:generated decisions -->\n- an entry a session appended by hand",
		1)
	edited = strings.Replace(edited,
		"<!-- /pop:generated decisions -->",
		"<!-- /pop:generated decisions -->\n\nA session's note below the region.",
		1)
	if err := os.WriteFile(mapPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "03", AnswerFile: answerFile(t, "The Go client.\n"),
	}); err != nil {
		t.Fatal(err)
	}

	mapMD := readFile(t, mapPath)
	if strings.Contains(mapMD, "an entry a session appended by hand") {
		t.Fatalf("a hand-written line inside the region survived:\n%s", mapMD)
	}
	for _, want := range []string{
		"A session's note above the region.",
		"A session's note below the region.",
		"Prose only a session writes.",
		"Fog stays here.",
		"- [Database](issues/01-first.md) — Postgres.",
		"- [Client](issues/03-third.md) — The Go client.",
	} {
		if !strings.Contains(mapMD, want) {
			t.Fatalf("map.md lost %q:\n%s", want, mapMD)
		}
	}
}

// TestConcurrentResolvesBothLandInTheIndex is the parallel-grilling case the
// generated region exists for: two windows resolving at once, one index holding
// both answers.
func TestConcurrentResolvesBothLandInTheIndex(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	// Claim first, serially: the point under test is the file write, and claiming
	// also opens the store so the race is not about lazy initialisation.
	for _, id := range []string{"01", "03"} {
		if _, err := ClaimTicket(d, "", resolveMapID, id); err != nil {
			t.Fatalf("claim %s: %v", id, err)
		}
	}

	answers := map[string]string{
		"01": answerFile(t, "Postgres.\n"),
		"03": answerFile(t, "The Go client.\n"),
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(answers))
	for id, path := range answers {
		wg.Add(1)
		go func(id, path string) {
			defer wg.Done()
			_, err := ResolveTicket(d, "", ResolveRequest{MapID: resolveMapID, Ticket: id, AnswerFile: path})
			errs <- err
		}(id, path)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent resolve: %v", err)
		}
	}

	decisions := regionBody(t, readFile(t, filepath.Join(mapDir, "map.md")), "decisions")
	for _, want := range []string{"issues/01-first.md", "issues/03-third.md"} {
		if !strings.Contains(decisions, want) {
			t.Fatalf("the decision index lost %q under concurrent resolves:\n%s", want, decisions)
		}
	}
	for _, id := range []string{"01", "03"} {
		if entry := manifestEntry(t, d, mapDir, id); entry.Status != TicketResolved {
			t.Fatalf("ticket %s = %+v, want resolved", id, entry)
		}
	}
}

// regionBody returns what one generated region of map.md currently holds.
func regionBody(t *testing.T, content, name string) string {
	t.Helper()
	open, closing := generatedRegionMarkers(name)
	start := strings.Index(content, open)
	end := strings.Index(content, closing)
	if start < 0 || end < start {
		t.Fatalf("map.md has no %q region:\n%s", name, content)
	}
	return content[start+len(open) : end]
}

// assertDrafts compares recorded draft lists, treating nil and an empty slice
// as the same "nothing recorded".
func assertDrafts(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

// TestResolveRecordsDraftPaths: --adr/--context are verified to exist and
// recorded on the manifest entry relative to the Map folder, whichever way the
// path was given.
func TestResolveRecordsDraftPaths(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	result, err := ResolveTicket(d, "", ResolveRequest{
		MapID:         resolveMapID,
		Ticket:        "01",
		AnswerFile:    answerFile(t, "Postgres.\n"),
		ADRDrafts:     []string{filepath.Join(mapDir, "adrs", "978d65fd-slug.md")},
		ContextDrafts: []string{"context/09-database.md"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertDrafts(t, result.Ticket.ADRDrafts, []string{"adrs/978d65fd-slug.md"}, "result ADRDrafts")
	assertDrafts(t, result.Ticket.ContextDrafts, []string{"context/09-database.md"}, "result ContextDrafts")

	entry := manifestEntry(t, d, mapDir, "01")
	assertDrafts(t, entry.ADRDrafts, []string{"adrs/978d65fd-slug.md"}, "manifest ADRDrafts")
	assertDrafts(t, entry.ContextDrafts, []string{"context/09-database.md"}, "manifest ContextDrafts")
}

// TestResolveReplacesDraftListsRatherThanAccumulating: a re-run overwrites the
// declared lists instead of appending to them, matching how the answer itself
// is replaced rather than accumulated.
func TestResolveReplacesDraftListsRatherThanAccumulating(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres.\n"),
		ADRDrafts: []string{"adrs/978d65fd-slug.md", "adrs/aaaa1111-other.md"},
	}); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if entry := manifestEntry(t, d, mapDir, "01"); len(entry.ADRDrafts) != 2 {
		t.Fatalf("manifest ADRDrafts after the first resolve = %v, want 2 entries", entry.ADRDrafts)
	}

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres, revised.\n"),
		ADRDrafts: []string{"adrs/aaaa1111-other.md"},
	}); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	assertDrafts(t, manifestEntry(t, d, mapDir, "01").ADRDrafts, []string{"adrs/aaaa1111-other.md"}, "manifest ADRDrafts after rerun")

	// A third run with no --adr at all clears the list rather than leaving the
	// previous one in place.
	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres, final.\n"),
	}); err != nil {
		t.Fatalf("third resolve: %v", err)
	}
	assertDrafts(t, manifestEntry(t, d, mapDir, "01").ADRDrafts, nil, "manifest ADRDrafts after clearing")
}

// TestResolveNeverParsesTheAnswerBodyForDrafts: a draft-shaped link inside the
// answer prose is not enough to record it — only a declared --adr/--context
// flag is.
func TestResolveNeverParsesTheAnswerBodyForDrafts(t *testing.T) {
	t.Parallel()
	d, storageDir := resolveFixture(t)
	mapDir := filepath.Join(storageDir, "maps", resolveMapID)

	if _, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01",
		AnswerFile: answerFile(t, "Postgres. See [adrs/978d65fd-slug.md](adrs/978d65fd-slug.md).\n"),
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	entry := manifestEntry(t, d, mapDir, "01")
	if len(entry.ADRDrafts) != 0 {
		t.Fatalf("a draft-shaped link in the answer body was recorded as a declared draft: %v", entry.ADRDrafts)
	}
}

// gitStatusMock wraps a fixture's Git seam so "status --porcelain" reports the
// given porcelain output while every other invocation (repository identity
// resolution) keeps behaving exactly as the fixture set it up.
func gitStatusMock(t *testing.T, d *Deps, porcelain string) {
	t.Helper()
	original := d.Tasks.Git
	d.Tasks.Git = &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return porcelain, nil
			}
			return original.CommandInDir(dir, args...)
		},
	}
}

// TestResolveWarnsButProceedsOnDirtyTree: refusing on a dirty tree was rejected
// (ADR-0171) because pop cannot tell an unrelated in-flight change from a stray
// fragment a grilling session left behind — so a dirty tree only ever warns.
func TestResolveWarnsButProceedsOnDirtyTree(t *testing.T) {
	t.Parallel()
	d, _ := resolveFixture(t)
	gitStatusMock(t, d, " M some/unrelated/file.go\n")

	result, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres.\n"),
	})
	if err != nil {
		t.Fatalf("resolve on a dirty tree refused: %v", err)
	}
	if !result.DirtyRepo {
		t.Fatal("result.DirtyRepo = false, want true on a dirty working tree")
	}
}

// TestResolveReportsACleanTree: the counterpart to the dirty case, pinning that
// the warning is conditional rather than unconditional.
func TestResolveReportsACleanTree(t *testing.T) {
	t.Parallel()
	d, _ := resolveFixture(t)
	gitStatusMock(t, d, "")

	result, err := ResolveTicket(d, "", ResolveRequest{
		MapID: resolveMapID, Ticket: "01", AnswerFile: answerFile(t, "Postgres.\n"),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.DirtyRepo {
		t.Fatal("result.DirtyRepo = true on a clean working tree")
	}
}
