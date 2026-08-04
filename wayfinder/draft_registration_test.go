package wayfinder

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

const draftMapID = "2026-08-03-drafts"

// draftFixture is a Map carrying three drafts, one of which a ticket records.
// The other two are what an assist session or a resolve that forgot its flags
// leaves behind.
func draftFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := "maps/" + draftMapID + "/"
	d, storageDir := registryFixture(t, map[string]string{
		dir + "map.md":                 resolveMapMarkdown,
		dir + "issues/01-first.md":     "## Question\n\nWhich database?\n",
		dir + "issues/02-second.md":    "## Question\n\nWhich schema?\n",
		dir + "adrs/978d65fd-known.md": "# Decision\n\nRecorded on ticket 01.\n",
		dir + "adrs/aaaa1111-loose.md": "# Decision\n\nNobody names this one.\n",
		dir + "context/02-glossary.md": "+ Schema — the shape of the store.\n",
		dir + "index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"Database","type":"grilling","status":"resolved",` +
			`"blocked_by":[],"adr_drafts":["adrs/978d65fd-known.md"]},` +
			`{"id":"02","file":"02-second.md","title":"Schema","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	})
	return d, filepath.Join(storageDir, "maps", draftMapID)
}

// warningNaming returns the one warning naming path, failing if the set does not
// hold exactly one such line.
func warningNaming(t *testing.T, warnings []string, path string) string {
	t.Helper()
	var found []string
	for _, w := range warnings {
		if strings.Contains(w, path) {
			found = append(found, w)
		}
	}
	if len(found) != 1 {
		t.Fatalf("warnings naming %q = %v, want exactly one (all: %v)", path, found, warnings)
	}
	return found[0]
}

func assertNoWarningNaming(t *testing.T, warnings []string, path string) {
	t.Helper()
	for _, w := range warnings {
		if strings.Contains(w, path) {
			t.Fatalf("warning names a recorded draft %q: %q", path, w)
		}
	}
}

// TestUnreferencedDraftIsAWarningNotAnError is the shape of the check: a draft
// nothing names is reported, a draft a ticket names is not, and neither verdict
// touches the manifest's validity — a Map with a loose draft is still a Map pop
// reads.
func TestUnreferencedDraftIsAWarningNotAnError(t *testing.T) {
	t.Parallel()
	d, mapDir := draftFixture(t)

	manifest, err := LoadMapManifest(d, mapDir)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Valid || len(manifest.Errors) != 0 {
		t.Fatalf("a loose draft must not invalidate the manifest: %v", manifest.Errors)
	}
	if len(manifest.Warnings) != 2 {
		t.Fatalf("warnings = %v, want the two unreferenced drafts", manifest.Warnings)
	}
	loose := warningNaming(t, manifest.Warnings, "adrs/aaaa1111-loose.md")
	if !strings.Contains(loose, "--adr") {
		t.Fatalf("warning %q must carry the corrective flag", loose)
	}
	glossary := warningNaming(t, manifest.Warnings, "context/02-glossary.md")
	if !strings.Contains(glossary, "--context") {
		t.Fatalf("warning %q must carry the corrective flag", glossary)
	}
	assertNoWarningNaming(t, manifest.Warnings, "978d65fd-known.md")
}

// TestDraftWarningsSurfaceOnTheLoadPath is the point of validating on load: no
// verb was run and nothing was re-registered, yet every read surface — the scan
// the dashboard paints from, the status table, the map detail — already says so.
func TestDraftWarningsSurfaceOnTheLoadPath(t *testing.T) {
	t.Parallel()
	d, _ := draftFixture(t)

	maps, err := ScanMaps(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 {
		t.Fatalf("maps = %d, want 1", len(maps))
	}
	m := maps[0]
	if m.Broken {
		t.Fatalf("a loose draft must not break the Map: %s", m.BrokenReason)
	}
	warningNaming(t, m.Warnings, "adrs/aaaa1111-loose.md")

	snap, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var table bytes.Buffer
	if err := RenderStatus(&table, snap); err != nil {
		t.Fatal(err)
	}
	if out := table.String(); !strings.Contains(out, "warning:") || !strings.Contains(out, "adrs/aaaa1111-loose.md") {
		t.Fatalf("`pop map status` must surface the orphan draft:\n%s", out)
	}

	var detail bytes.Buffer
	if err := RenderShow(&detail, m, nil); err != nil {
		t.Fatal(err)
	}
	if out := detail.String(); !strings.Contains(out, "warning: adrs/aaaa1111-loose.md") {
		t.Fatalf("`pop map status <map-id>` must surface the orphan draft:\n%s", out)
	}

	sections := sectionsFor(m, nil)
	if len(sections) == 0 || sections[0].Title != "Warnings" ||
		!strings.Contains(sections[0].Body, "adrs/aaaa1111-loose.md") {
		t.Fatalf("the Work dashboard's detail sections must lead with the warning: %+v", sections)
	}
}

// TestRegisterReportsDraftsWithoutRefusing keeps the fix loop honest: the
// registration gate is what pop cannot read, and a loose draft is readable.
func TestRegisterReportsDraftsWithoutRefusing(t *testing.T) {
	t.Parallel()
	d, _ := draftFixture(t)

	result, err := RegisterMap(d, "", draftMapID)
	if err != nil {
		t.Fatalf("register must not refuse over a loose draft: %v", err)
	}
	warningNaming(t, result.Warnings, "adrs/aaaa1111-loose.md")
}

// TestResolveStillWritesWithLooseDrafts is the decision this slice rejected the
// alternative for: resolve reports the orphan and writes anyway, and the drafts
// the call itself records are not reported — the manifest is re-checked as
// written, not as it was read.
func TestResolveStillWritesWithLooseDrafts(t *testing.T) {
	t.Parallel()
	d, mapDir := draftFixture(t)
	mustRegister(t, d, draftMapID)
	asWindow(d, "pane:%1", at(9))

	result, err := ResolveTicket(d, "", ResolveRequest{
		MapID:         draftMapID,
		Ticket:        "02",
		AnswerFile:    answerFile(t, "One table per aggregate.\n"),
		ContextDrafts: []string{"context/02-glossary.md"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if entry := manifestEntry(t, d, mapDir, "02"); entry.Status != TicketResolved {
		t.Fatalf("ticket 02 = %+v, want resolved — the write must not be withheld", entry)
	}
	assertNoWarningNaming(t, result.Warnings, "context/02-glossary.md")
	warningNaming(t, result.Warnings, "adrs/aaaa1111-loose.md")
}

// TestArrivalReportsLooseDrafts covers the last moment anybody looks at a Map.
func TestArrivalReportsLooseDrafts(t *testing.T) {
	t.Parallel()
	d, _ := draftFixture(t)
	mustRegister(t, d, draftMapID)

	result, err := ArriveMap(d, "", draftMapID)
	if err != nil {
		t.Fatalf("arrive: %v", err)
	}
	warningNaming(t, result.Warnings, "adrs/aaaa1111-loose.md")
}
