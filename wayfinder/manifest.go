package wayfinder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// MapManifestFileName is the machine-readable half of a Map, sitting beside
// map.md. It mirrors a Task set's index.json: every consumer reads ticket
// metadata from here instead of hand-parsing N ticket markdown files.
const MapManifestFileName = "index.json"

// The manifest's two top-level keys, and the shape a ticket filename must take.
// The parser, the writer, the validator's diagnostics and the authoring guide
// all say them from here, so a printed rule is the enforced one.
const (
	manifestTicketsKey     = "tickets"
	manifestSpawnedSetsKey = "spawned_sets"
	ticketFileShape        = "NN-<slug>.md"
)

// The two draft directories of a Map folder: the repo-facing artifacts a
// decision produces, which a resolution records in adr_drafts / context_drafts.
// The validator walks them, the authoring guide names them, and `--adr` /
// `--context` record paths under them.
const (
	adrDraftsDirName     = "adrs"
	contextDraftsDirName = "context"
)

// mapDraftDirs are the directories every draft file must live in to be reachable
// from a manifest entry, in the order a report reads best.
var mapDraftDirs = []string{adrDraftsDirName, contextDraftsDirName}

var (
	manifestTicketStatuses = ticketStatusSet(manifestTicketStatusOrder)
	manifestTicketTypes    = ticketTypeSet(manifestTicketTypeOrder)
)

func ticketStatusSet(values []TicketStatus) map[TicketStatus]bool {
	set := make(map[TicketStatus]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

func ticketTypeSet(values []TicketType) map[TicketType]bool {
	set := make(map[TicketType]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// ManifestTicket is one Decision ticket entry in a Map's index.json. Status is
// open | resolved only: a claim is an owner plus a timestamp in pop.db, not a
// file state, so it has no representation here.
type ManifestTicket struct {
	ID     string       `json:"id"`
	File   string       `json:"file"`
	Title  string       `json:"title"`
	Type   TicketType   `json:"type"`
	Status TicketStatus `json:"status"`
	// OutOfScope splits the two resolutions. Both end a ticket, but one is a step
	// on the route walked and the other is a boundary of it, and the generated
	// sections of map.md are rebuilt from this bit — so which section a resolved
	// ticket renders into is a manifest fact, never a guess at its prose.
	OutOfScope    bool     `json:"out_of_scope,omitempty"`
	BlockedBy     []string `json:"blocked_by"`
	ADRDrafts     []string `json:"adr_drafts,omitempty"`
	ContextDrafts []string `json:"context_drafts,omitempty"`
}

// MapManifest is a parsed and validated Map manifest.
type MapManifest struct {
	// Dir is the Map folder holding both index.json and issues/.
	Dir     string
	Path    string
	Tickets []ManifestTicket
	// SpawnedSets holds the ids of Task sets this Map handed off, in the order
	// they were spawned. Never nil — an absent key reads as an empty array.
	SpawnedSets []string
	// Unknown preserves keys pop does not read so a rewrite never strips them.
	Unknown map[string]json.RawMessage
	Errors  []string
	// Warnings are manifest problems that are reported but never refused over:
	// today, draft files no ticket claims. They are advisory because pop cannot
	// tell an artifact somebody forgot to record from one still being written, and
	// because the sessions that leave them behind — assist above all — resolve
	// nothing, so there is no write to withhold that would help (ADR-0171's
	// dirty-tree precedent). Valid is a function of Errors alone.
	Warnings []string
	Valid    bool
}

// MapManifestPath returns the manifest path for a Map folder.
func MapManifestPath(mapDir string) string {
	return filepath.Join(mapDir, MapManifestFileName)
}

// LoadMapManifest reads and validates a Map's index.json. A missing manifest is
// reported as os.ErrNotExist so callers can fall back to header parsing; every
// other problem — unreadable, unparseable, invalid — comes back as a manifest
// carrying per-problem diagnostics in Errors, which render the Map MALFORMED.
func LoadMapManifest(d *Deps, mapDir string) (*MapManifest, error) {
	m := &MapManifest{
		Dir:         mapDir,
		Path:        MapManifestPath(mapDir),
		SpawnedSets: []string{},
		Unknown:     map[string]json.RawMessage{},
	}

	data, err := d.FS.ReadFile(m.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		m.Errors = append(m.Errors, fmt.Sprintf("read manifest: %v", err))
		return m, nil
	}

	if err := parseMapManifestJSON(data, m); err != nil {
		m.Errors = append(m.Errors, err.Error())
		return m, nil
	}

	validateMapManifest(d, m)
	m.Valid = len(m.Errors) == 0
	return m, nil
}

func parseMapManifestJSON(data []byte, m *MapManifest) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	ticketsRaw, ok := raw[manifestTicketsKey]
	if !ok {
		return fmt.Errorf("missing tickets array")
	}
	if err := json.Unmarshal(ticketsRaw, &m.Tickets); err != nil {
		return fmt.Errorf("parse tickets: %w", err)
	}

	if spawnedRaw, ok := raw[manifestSpawnedSetsKey]; ok {
		var spawned []string
		if err := json.Unmarshal(spawnedRaw, &spawned); err != nil {
			return fmt.Errorf("parse spawned_sets: %w", err)
		}
		for _, id := range spawned {
			if id != "" {
				m.SpawnedSets = append(m.SpawnedSets, id)
			}
		}
	}

	for k, v := range raw {
		if k == manifestTicketsKey || k == manifestSpawnedSetsKey {
			continue
		}
		m.Unknown[k] = v
	}
	return nil
}

func validateMapManifest(d *Deps, m *MapManifest) {
	onDisk, err := listTicketMarkdown(d, filepath.Join(m.Dir, issuesDirName))
	if err != nil {
		m.Errors = append(m.Errors, fmt.Sprintf("list issues: %v", err))
	}

	ids := make(map[string]bool, len(m.Tickets))
	files := make(map[string]bool, len(m.Tickets))
	claimed := make(map[string]bool, len(m.Tickets))

	for i, t := range m.Tickets {
		if t.ID == "" {
			m.Errors = append(m.Errors, fmt.Sprintf("ticket[%d]: missing id", i))
			continue
		}
		if ids[t.ID] {
			m.Errors = append(m.Errors, fmt.Sprintf("duplicate ticket id %q", t.ID))
		}
		ids[t.ID] = true

		switch {
		case t.File == "":
			m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: missing file", t.ID))
		case strings.ContainsAny(t.File, `/\`):
			m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: file must be a name under issues/, got %q", t.ID, t.File))
		case !ticketFilePattern.MatchString(t.File):
			m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: file %q does not match %s", t.ID, t.File, ticketFileShape))
		default:
			if files[t.File] {
				m.Errors = append(m.Errors, fmt.Sprintf("duplicate ticket file %q", t.File))
			}
			files[t.File] = true
			if !onDisk[t.File] {
				m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: missing markdown file %q", t.ID, t.File))
			} else {
				claimed[t.File] = true
			}
		}

		if !manifestTicketTypes[t.Type] {
			m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: unknown type %q", t.ID, t.Type))
		}
		if !manifestTicketStatuses[t.Status] {
			m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: unknown status %q", t.ID, t.Status))
		}
	}

	for _, t := range m.Tickets {
		for _, blocker := range t.BlockedBy {
			if !ids[blocker] {
				m.Errors = append(m.Errors, fmt.Sprintf("ticket %q: unresolved blocker %q", t.ID, blocker))
			}
		}
	}

	var orphans []string
	for name := range onDisk {
		if !claimed[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		m.Errors = append(m.Errors, fmt.Sprintf("%s: no manifest entry", name))
	}

	validateDraftRegistration(d, m)
}

// validateDraftRegistration runs the orphan check in the direction resolution
// cannot: validateDraftPaths proves a declared draft exists, and this proves an
// existing draft is declared. Without it a draft written during grilling and
// never passed to `resolve --adr/--context` is a file nothing references, and the
// handoff — which mints checkboxes from the manifest arrays — drops it silently.
func validateDraftRegistration(d *Deps, m *MapManifest) {
	declared := make(map[string]bool)
	for _, t := range m.Tickets {
		for _, list := range [][]string{t.ADRDrafts, t.ContextDrafts} {
			for _, path := range list {
				declared[normalizeDraftPath(path)] = true
			}
		}
	}

	for _, dir := range mapDraftDirs {
		names, err := listDraftFiles(d, filepath.Join(m.Dir, dir))
		if err != nil {
			m.Warnings = append(m.Warnings, fmt.Sprintf("list %s/: %v", dir, err))
			continue
		}
		for _, name := range names {
			rel := dir + "/" + name
			if declared[rel] {
				continue
			}
			m.Warnings = append(m.Warnings, fmt.Sprintf(
				"%s: no ticket names this draft; record it with `pop map resolve --%s`",
				rel, draftFlagFor(dir)))
		}
	}
}

// draftFlagFor names the resolve flag that records a draft in the directory it
// belongs to, so the warning carries the corrective rather than the fact alone.
func draftFlagFor(dir string) string {
	if dir == adrDraftsDirName {
		return "adr"
	}
	return "context"
}

// normalizeDraftPath reduces a recorded draft to the map-relative slash path the
// directory walk produces, so `./adrs/x.md` and `adrs/x.md` name one file.
func normalizeDraftPath(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
}

// listDraftFiles lists one draft directory, sorted. Every file counts, not only
// markdown: a draft is whatever a decision produced, and a stray non-markdown
// artifact is exactly as droppable at handoff as a stray .md.
func listDraftFiles(d *Deps, dir string) ([]string, error) {
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func listTicketMarkdown(d *Deps, issuesDir string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := d.FS.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		out[entry.Name()] = true
	}
	return out, nil
}

// WriteMapManifest writes a Map manifest atomically, preserving unknown keys and
// always emitting spawned_sets and blocked_by as arrays rather than null.
func WriteMapManifest(d *Deps, m *MapManifest) error {
	out := make(map[string]json.RawMessage, len(m.Unknown)+2)
	for k, v := range m.Unknown {
		out[k] = v
	}

	tickets := make([]ManifestTicket, len(m.Tickets))
	for i, t := range m.Tickets {
		if t.BlockedBy == nil {
			t.BlockedBy = []string{}
		}
		tickets[i] = t
	}
	ticketsData, err := json.Marshal(tickets)
	if err != nil {
		return err
	}
	out[manifestTicketsKey] = ticketsData

	spawned := m.SpawnedSets
	if spawned == nil {
		spawned = []string{}
	}
	spawnedData, err := json.Marshal(spawned)
	if err != nil {
		return err
	}
	out[manifestSpawnedSetsKey] = spawnedData

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return tasks.WriteAtomicWith(&tasks.Deps{FS: d.FS}, m.Path, data, 0o644)
}

// MalformedReason joins the manifest's diagnostics into one MALFORMED summary.
func (m *MapManifest) MalformedReason() string {
	return strings.Join(m.Errors, "; ")
}

// Tickets converts manifest entries into the scanner's ticket model, ordered by
// ticket number. Number and slug come from the entry's filename, which the
// manifest guarantees is NN-<slug>.md.
func (m *MapManifest) ToTickets() []Ticket {
	tickets := make([]Ticket, 0, len(m.Tickets))
	for _, entry := range m.Tickets {
		tickets = append(tickets, entry.ToTicket())
	}
	sortTickets(tickets)
	return tickets
}

// ToTicket converts one manifest entry into the scanner's ticket model.
func (t ManifestTicket) ToTicket() Ticket {
	ticket := Ticket{
		ID:            t.ID,
		File:          t.File,
		Title:         t.Title,
		Type:          t.Type,
		Status:        t.Status,
		OutOfScope:    t.OutOfScope,
		BlockedBy:     t.BlockedBy,
		ADRDrafts:     t.ADRDrafts,
		ContextDrafts: t.ContextDrafts,
	}
	base := filepathBase(t.File)
	if match := ticketFilePattern.FindStringSubmatch(base); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil {
			ticket.Number = n
		}
		if dash := strings.Index(base, "-"); dash > 0 {
			ticket.Slug = strings.TrimSuffix(base[dash+1:], ".md")
		}
	} else if n, err := strconv.Atoi(t.ID); err == nil {
		ticket.Number = n
	}
	return ticket
}
