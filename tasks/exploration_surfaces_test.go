package tasks

import (
	"strings"
	"testing"
	"time"
)

// explorationProse stands in for a whole map: the text that must never appear
// in a prompt, since the report is handed over as a path however long it is.
const explorationProse = "`tasks/prompt.go` owns every prompt builder; the text sits in `tasks/prompts`."

func seedExplorationReport(t *testing.T, d *Deps, m *Manifest) string {
	t.Helper()
	body := explorationReport.renderDocument(time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		"demo", "abc123abc123", "", "claude", "## Where the prompts live\n\n"+explorationProse)
	path, err := writeExplorationReport(d, m.Dir, body)
	if err != nil {
		t.Fatalf("writeExplorationReport: %v", err)
	}
	return path
}

// TestExplorationReportReachesTheBuilder drives the slice the pass exists for:
// an implement attempt on an explored set is handed the report's path and the
// one edit it may make to it, an Assist session is handed the same path, and
// neither is handed a word of the document itself.
func TestExplorationReportReachesTheBuilder(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedExplorationReport(t, d, m)

	builder := BuildAgentPrompt(d, m.Dir+"/01-a.md", "/rt", "")
	for _, want := range []string{path, "read the file yourself", "Correct it only where this attempt falsified", "change nothing else in it"} {
		if !strings.Contains(builder, want) {
			t.Fatalf("implementer prompt missing %q:\n%s", want, builder)
		}
	}
	// The edit boundary has to still read true with the report in it: the task
	// file can no longer be "the one file you also edit".
	if strings.Contains(builder, "the one file you also edit") {
		t.Fatalf("implementer prompt keeps an edit boundary the report falsifies:\n%s", builder)
	}
	// The prior-art check is the same words for every builder, explored or not.
	if !strings.Contains(builder, priorArtBlock(t)) {
		t.Fatalf("implementer prompt lost the prior-art block:\n%s", builder)
	}

	assist := BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", "")
	if !strings.Contains(assist, path) {
		t.Fatalf("assist prompt missing the report path:\n%s", assist)
	}
	for surface, text := range map[string]string{"implementer prompt": builder, "assist prompt": assist} {
		if strings.Contains(text, explorationProse) {
			t.Fatalf("%s inlined the exploration report:\n%s", surface, text)
		}
	}
}

// TestUnexploredSetIsToldNothing pins the quiet arm: a set with no report — or
// with an empty file where one would be — says nothing about one, and the
// builder's frame reads as it did before the pass existed.
func TestUnexploredSetIsToldNothing(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	taskPath := m.Dir + "/01-a.md"

	noReport := BuildAgentPrompt(d, taskPath, "/rt", "")
	if err := d.FS.WriteFile(m.Dir+"/"+ExplorationFileName, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write empty report: %v", err)
	}
	for surface, text := range map[string]string{
		"implementer prompt (no report)":    noReport,
		"implementer prompt (empty report)": BuildAgentPrompt(d, taskPath, "/rt", ""),
		"assist prompt":                     BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", ""),
	} {
		if strings.Contains(strings.ToLower(text), "exploration report") {
			t.Fatalf("%s mentions a report the set does not have:\n%s", surface, text)
		}
	}
	if !strings.Contains(noReport, "The task file\nabove is the one file you also edit") {
		t.Fatalf("implementer prompt lost its edit boundary:\n%s", noReport)
	}
}

// priorArtBlock is the prior-art check as the unexplored frame states it, which
// is the byte-for-byte text the explored frame must also carry.
func priorArtBlock(t *testing.T) string {
	t.Helper()
	plain := BuildAgentPrompt(nil, "/pop/tasks/demo/01-a.md", "/rt", "")
	_, rest, ok := strings.Cut(plain, "## Before you hand-write mechanism")
	if !ok {
		t.Fatalf("the frame no longer states the prior-art check:\n%s", plain)
	}
	block, _, ok := strings.Cut(rest, "\n\nThis attempt is a single")
	if !ok {
		t.Fatalf("the prior-art block no longer ends where it did:\n%s", plain)
	}
	return block
}
