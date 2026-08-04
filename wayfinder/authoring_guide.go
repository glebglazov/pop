package wayfinder

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AuthoringGuide is what `pop map authoring-guide` prints: how to hand-write a
// Map. It is generated rather than written down so it cannot disagree with the
// validator — every status word, ticket enum, filename, pattern and generated
// marker below comes from the same value LoadMapManifest and ParseMapMarkdown
// read. A constant changes and the guide changes with it, in the same build.
//
// The guide describes the artifact, not a session's workflow: charting, assist
// and a graduating fog ticket all write the same files, and the behavioural
// rules around them (claiming, resolution, handoff) stay in the Work-store doc.
func AuthoringGuide() string {
	var b strings.Builder

	fmt.Fprintf(&b, `# Authoring a Map by hand

pop owns a Map's structure and validation; you write the files. There is no
authoring payload and no create verb: write %s, the ticket files and
%s yourself, then run `+"`pop map register <map-id>`"+`, which validates the manifest
and reports every problem it finds at once. It is re-runnable — fix what it
names and run it again.

**You pick the ids, and you write everything in one pass.** There is no
create-then-wire second pass. That convention comes from trackers where a
*server* mints issue ids, so a ticket cannot reference one that does not exist
yet; in files you choose the ticket numbers yourself, so write every ticket
body and then the manifest — blocking edges included — in a single pass.

## Storage layout

Maps live under the maps/ sibling of tasks/ in this repository's Work store. Run
`+"`pop work show-path`"+` once to resolve it. A Map exists because its folder
exists; there is no separate create step.

    $(pop work show-path)/maps/<map-id>/
    ├── %s
    ├── %s
    ├── %s/
    │   ├── 01-<slug>.md
    │   └── 02-<slug>.md
    ├── %s/             (ADR drafts, <8hex>-<slug>.md)
    └── %s/          (glossary-op drafts, NN-<slug>.md)

- `+"`<map-id>`"+` is `+"`<YYYY-MM-DD-slug>`"+` (e.g. `+"`2026-07-19-work-dashboard`"+`),
  and it is the id every `+"`pop map`"+` verb takes.
- Ticket files are `+"`%s`"+` under `+"`%s/`"+`, where NN is a zero-padded
  ticket number. The validator matches them against `+"`%s`"+`.
- Open tickets are never listed in %s: they are files, discovered by reading
  the directory.

## %s

`+"```markdown"+`
%s
`+"```"+`

The `+"`Status:`"+` line sits above the first heading and takes
%s.
Charting writes `+"`Status: %s`"+`; `+"`arrived`"+` is written by
`+"`pop map arrive`"+` and reversed by `+"`pop map open`"+`, never by hand. Any
other word renders the Map BROKEN with the fix printed.

**%s has two writers.** The prose sections are yours. The regions between the
`+"`pop:generated`"+` markers are pop's: it rebuilds them from %s and the
ticket answers on every resolve, so anything written inside a region is lost on
the next one. Write prose outside the markers — it survives — and never
hand-edit a decision line.

Generated regions, and the headings they live under:

%s
## Ticket files

Ticket markdown holds **prose only**. Id, title, type, status, blockers and
drafts live in %s, which every consumer reads.

`+"```markdown"+`
%s
`+"```"+`

The answer body between those markers belongs to `+"`pop map resolve`"+`, which
replaces it whole on every run, headings and all. Fix an answer by resolving
again with a corrected `+"`--answer-file`"+`, not by hand-editing the ticket.

## %s

One entry per ticket file, plus the set of Task sets this Map has handed off:

`+"```json"+`
%s
`+"```"+`

Per-ticket fields:

- `+"`id`"+` — the zero-padded ticket number (e.g. `+"`01`"+`); the identifier
  `+"`blocked_by`"+` references.
- `+"`file`"+` — the ticket's markdown filename under `+"`%s/`"+`, a bare name
  matching `+"`%s`"+` (no directory part).
- `+"`title`"+` — a short human label, used by every render that lists tickets.
- `+"`type`"+` — %s.
- `+"`status`"+` — %s. Author every ticket
  `+"`%s`"+`; a claim is a pop.db row owned by the grilling pane, not a file
  state, so it has no representation here.
- `+"`blocked_by`"+` — array of blocker ids; empty array if none. A ticket is
  unblocked when every blocker is `+"`%s`"+`.
- `+"`out_of_scope`"+` — set by `+"`pop map out-of-scope`"+`, never at authoring
  time. It decides which generated section a resolved ticket renders into.
- `+"`adr_drafts`"+` / `+"`context_drafts`"+` — draft files a resolution
  produced, relative to the Map folder (under `+"`%s/`"+` and
  `+"`%s/`"+`). Written by `+"`pop map resolve --adr/--context`"+`, never by
  hand.

**Every draft must be named by some ticket.** The registration is what carries a
draft through handoff — the spawned set mints its checkboxes from these arrays —
so a file under `+"`%s/`"+` or `+"`%s/`"+` that no ticket claims is
dropped at handoff with nothing said. Validation runs the check on every read of
the Map and reports each unclaimed draft; it is a warning rather than a refusal,
because a draft still being written is indistinguishable from one forgotten. The
fix is to resolve again with the draft passed to
`+"`--adr`"+` / `+"`--context`"+` — a re-resolve replaces the recorded set
rather than appending to it.

Set-level key:

- `+"`spawned_sets`"+` — the Task sets this Map handed off, appended by
  `+"`pop map spawned`"+`. Author it as an empty array and leave it alone.

Keys pop does not read ride through a rewrite untouched.

## What a session may write

One flat contract, and every wayfinding session holds it: charting a fresh Map,
`+"`pop map assist <map-id>`"+` on a live one, and a session graduating fog out of
`+"`%s`"+` all write the same set. There is no permission one of them holds
alone.

Writable:

- ticket files under `+"`%s/`"+` — new ones, and an existing ticket's
  `+"`## Question`"+` amended in place;
- the manifest's authoring fields, `+"`blocked_by`"+` edges included — wired and
  unwired;
- %s's prose sections (%s) — the
  human-owned half of the file.

Off limits:

- **everything between the `+"`pop:generated`"+` markers**, in %s and in a
  ticket's `+"`## %s`"+` alike. pop rebuilds those regions from %s on
  every resolve, so a hand-edit there is not wrong so much as lost.
- **the repository under study.** Wayfinding produces decisions, not code: no
  source edit, no commit, no branch. Drafts a decision produces belong in the
  Map's own folder and are recorded by
  `+"`pop map resolve --adr/--context`"+`.

Which sessions may *resolve* a ticket is a workflow rule rather than a fact about
these files, so it lives in the Work-store doc's `+"`Resolution`"+` section beside
the one-non-research-ticket-per-session rule it protects.

## What registration enforces

`+"`pop map register`"+` reports the whole fix list at once, not the first item:

- every ticket has an id, and no two share one;
- every `+"`file`"+` is a bare name matching `+"`%s`"+`, unique, and present on
  disk under `+"`%s/`"+`;
- every `+"`type`"+` and `+"`status`"+` is one of the words above;
- every `+"`blocked_by`"+` id names a ticket in the manifest;
- every markdown file under `+"`%s/`"+` has a manifest entry — a ticket file
  nothing claims is reported as an orphan rather than silently ignored.
`,
		mapFileName, MapManifestFileName,
		mapFileName, MapManifestFileName, issuesDirName, adrDraftsDirName, contextDraftsDirName,
		ticketFileShape, issuesDirName, ticketFilePattern.String(),
		mapFileName,
		mapFileName,
		mapMarkdownTemplate(),
		enumList(mapStatusWords()), MapActive,
		mapFileName, MapManifestFileName,
		generatedRegionList(),
		MapManifestFileName,
		ticketMarkdownTemplate(),
		MapManifestFileName,
		mapManifestExample(),
		issuesDirName, ticketFileShape,
		enumList(ticketTypeWords()),
		enumList(ticketStatusWords()), TicketOpen,
		TicketResolved,
		adrDraftsDirName, contextDraftsDirName,
		adrDraftsDirName, contextDraftsDirName,
		fogSectionName, issuesDirName,
		mapFileName, proseSectionList(),
		mapFileName, answerSectionName, MapManifestFileName,
		ticketFileShape, issuesDirName, issuesDirName,
	)

	return b.String()
}

// mapMarkdownTemplate renders the map.md skeleton with pop's generated regions
// in place, so a session copying it starts with markers pop already recognises
// rather than letting the first resolve take a hand-written section over.
func mapMarkdownTemplate() string {
	lines := []string{
		"Status: " + string(MapActive),
		"",
		"## " + destinationSectionName,
		"",
		"<one or two lines — every session orients here first>",
		"",
		"## " + notesSectionName,
		"",
		"<domain; skills to consult; standing preferences>",
		"",
	}
	for _, region := range mapGeneratedRegions {
		open, close := generatedRegionMarkers(region.name)
		lines = append(lines, "## "+region.heading, "", open, close, "")
		if region.name == regionDecisions.name {
			lines = append(lines,
				"## "+fogSectionName,
				"",
				"<fog — graduates into tickets as the frontier advances>",
				"")
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// ticketMarkdownTemplate renders a ticket file: the question a session writes,
// and the answer region pop owns.
func ticketMarkdownTemplate() string {
	open, close := generatedRegionMarkers(answerRegionName)
	return strings.Join([]string{
		"## Question",
		"",
		"<the decision or investigation this ticket resolves>",
		"",
		"## " + answerSectionName,
		"",
		open,
		"",
		"<written by `pop map resolve` — prose answer, links to assets>",
		"",
		close,
	}, "\n")
}

// generatedRegionList prints each pop-owned region as its heading plus the
// marker pair that delimits it.
func generatedRegionList() string {
	var b strings.Builder
	for _, region := range mapGeneratedRegions {
		open, close := generatedRegionMarkers(region.name)
		fmt.Fprintf(&b, "- `## %s`\n  `%s` … `%s`\n", region.heading, open, close)
	}
	return b.String()
}

// mapManifestExample marshals real manifest values, so the printed keys are the
// struct tags LoadMapManifest unmarshals and can never be a stale transcription.
func mapManifestExample() string {
	tickets := []ManifestTicket{
		{
			ID:        "01",
			File:      "01-storage-shape.md",
			Title:     "Storage shape",
			Type:      TicketGrilling,
			Status:    TicketResolved,
			BlockedBy: []string{},
		},
		{
			ID:        "02",
			File:      "02-read-path.md",
			Title:     "Read path",
			Type:      TicketTask,
			Status:    TicketOpen,
			BlockedBy: []string{"01"},
		},
	}
	ticketsJSON, err := json.Marshal(tickets)
	if err != nil {
		return ""
	}
	out := map[string]json.RawMessage{
		manifestTicketsKey:     ticketsJSON,
		manifestSpawnedSetsKey: json.RawMessage("[]"),
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func mapStatusWords() []string {
	words := make([]string, 0, len(authorableMapStatuses))
	for _, s := range authorableMapStatuses {
		words = append(words, string(s))
	}
	return words
}

func ticketTypeWords() []string {
	words := make([]string, 0, len(manifestTicketTypeOrder))
	for _, t := range manifestTicketTypeOrder {
		words = append(words, string(t))
	}
	return words
}

func ticketStatusWords() []string {
	words := make([]string, 0, len(manifestTicketStatusOrder))
	for _, s := range manifestTicketStatusOrder {
		words = append(words, string(s))
	}
	return words
}

// proseSectionList names map.md's human-owned sections as a sentence, from the
// same constants the template lays out.
func proseSectionList() string {
	quoted := make([]string, 0, len(mapProseSections))
	for _, name := range mapProseSections {
		quoted = append(quoted, "`"+name+"`")
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

// enumList renders an enum the way both guides say one: `a` | `b` | `c`.
func enumList(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, "`"+w+"`")
	}
	return strings.Join(quoted, " | ")
}
