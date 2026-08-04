package wayfinder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// decisionGistMaxLen keeps one decision to one readable line of map.md. The
// answer itself is a click away; the index exists to be skimmed.
const decisionGistMaxLen = 120

// ResolveRequest names the ticket a resolution verb acts on and carries the
// verdict: an answer read from a file, or the reason a ticket is out of scope.
type ResolveRequest struct {
	MapID  string
	Ticket string
	// AnswerFile holds the `## Answer` body. Prose arrives as a file because an
	// answer is paragraphs, not a shell argument.
	AnswerFile string
	// Reason is the out-of-scope verdict, given inline: a scope boundary is one
	// sentence.
	Reason string
	// ADRDrafts and ContextDrafts name draft files this decision produced,
	// declared as flags rather than parsed from the answer body: pop never reads
	// prose looking for links, so a draft is either declared or it does not exist
	// as far as the tooling is concerned. Repeatable; replaces whatever a
	// previous resolve recorded rather than accumulating.
	ADRDrafts     []string
	ContextDrafts []string
}

// ResolveResult reports one completed resolution.
type ResolveResult struct {
	MapID  string
	Ticket Ticket
	Path   string
	// OutOfScope says which generated section the ticket rendered into.
	OutOfScope bool
	// Replaced reports that the ticket was already resolved and this run replaced
	// its answer — the re-run that fixes a mistake, not a second answer.
	Replaced bool
	// ReleasedClaim names the grilling pane that held the ticket, if any.
	ReleasedClaim string
	// DirtyRepo reports that the repository working tree carried an uncommitted
	// change at resolution. pop cannot tell an unrelated in-flight change from a
	// stray fragment a grilling session left behind, so this only ever warns —
	// resolving still proceeds.
	DirtyRepo bool
}

// ResolveTicket records a decision: it writes the ticket's `## Answer`, flips its
// manifest entry to resolved, and re-renders map.md's generated index — all
// three, or none of them. Everything is validated and every new file body is
// built before the first byte is written, so a refusal leaves the Map exactly as
// it was; a re-run replaces the answer rather than appending to it.
func ResolveTicket(d *Deps, cwd string, req ResolveRequest) (*ResolveResult, error) {
	path := strings.TrimSpace(req.AnswerFile)
	if path == "" {
		return nil, errors.New("an answer needs --answer-file <path>")
	}
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read answer file %s: %w", path, err)
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return nil, fmt.Errorf("answer file %s is empty", path)
	}
	return resolve(d, cwd, req, body, false)
}

// RuleOutOfScope is the second resolution path. It ends the ticket exactly as
// ResolveTicket does, but renders it under `Out of scope`: a boundary of the
// destination is not a step on the route walked to it, so the two never share a
// section — and that is why this is a verb of its own rather than a flag.
func RuleOutOfScope(d *Deps, cwd string, req ResolveRequest) (*ResolveResult, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, errors.New("ruling a ticket out of scope needs --reason <why>")
	}
	return resolve(d, cwd, req, reason, true)
}

func resolve(d *Deps, cwd string, req ResolveRequest, body string, outOfScope bool) (*ResolveResult, error) {
	m, err := findClaimableMap(d, cwd, req.MapID)
	if err != nil {
		return nil, err
	}
	ticketID, err := ticketIDFromArg(req.Ticket)
	if err != nil {
		return nil, err
	}
	if _, ok := findTicket(m.Tickets, ticketID); !ok {
		return nil, fmt.Errorf("map %q has no ticket %q; valid: %s", m.ID, ticketID, ticketIDList(m.Tickets))
	}

	adrDrafts, err := validateDraftPaths(d, m.Dir, "--adr", req.ADRDrafts)
	if err != nil {
		return nil, err
	}
	contextDrafts, err := validateDraftPaths(d, m.Dir, "--context", req.ContextDrafts)
	if err != nil {
		return nil, err
	}

	// Advisory only (ADR-0171): pop cannot tell an unrelated in-flight change
	// from a stray fragment a grilling session left behind, so a git error or a
	// dirty tree never blocks the write — it only warns.
	dirty, _ := tasks.RuntimeIsDirty(d.taskDeps(), m.Dir)

	var result *ResolveResult
	err = withMapLock(d, m.ID, func() error {
		var inner error
		result, inner = writeResolution(d, m, ticketID, body, outOfScope, adrDrafts, contextDrafts)
		return inner
	})
	if err != nil {
		return nil, err
	}
	result.DirtyRepo = dirty
	return result, nil
}

// validateDraftPaths verifies each declared draft file exists before anything is
// written — a typo in --adr/--context is a mistake caught immediately, not a
// dangling manifest entry. Recorded paths are relative to the Map folder, where
// grill-with-map actually writes them; an absolute path is accepted but recorded
// relative to the same root.
func validateDraftPaths(d *Deps, mapDir, flag string, paths []string) ([]string, error) {
	recorded := make([]string, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs := raw
		rel := filepath.ToSlash(filepath.Clean(raw))
		if filepath.IsAbs(raw) {
			if r, err := filepath.Rel(mapDir, raw); err == nil {
				rel = filepath.ToSlash(r)
			}
		} else {
			abs = filepath.Join(mapDir, raw)
		}
		info, err := d.FS.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("%s %s does not exist", flag, raw)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s %s is a directory, not a draft file", flag, raw)
		}
		recorded = append(recorded, rel)
	}
	return recorded, nil
}

// withMapLock serialises a Map's three-file write across processes. Several
// grilling panes resolve into one map.md, and each resolve is a read of the
// manifest followed by a rewrite of it and of the index — a sequence no atomic
// rename can make safe on its own, because the second writer must see the first
// writer's entry to re-render it.
func withMapLock(d *Deps, mapID string, fn func() error) error {
	td := d.taskDeps()
	return tasks.WithFileLock(td, tasks.LockPathWith(td, "map-"+mapID), fmt.Sprintf("map %s lock", mapID), fn)
}

// writeResolution performs the three writes under the Map's lock. It re-reads the
// manifest inside the lock — the copy the caller validated against may be a
// concurrent window's predecessor — and writes the ticket markdown before the
// manifest, so an interrupted resolve leaves an unresolved ticket carrying its
// answer rather than a resolved ticket carrying none.
func writeResolution(d *Deps, m Map, ticketID, body string, outOfScope bool, adrDrafts, contextDrafts []string) (*ResolveResult, error) {
	manifest, err := LoadMapManifest(d, m.Dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("map %q has no %s; run `pop map register %s` first", m.ID, MapManifestFileName, m.ID)
	}
	if err != nil {
		return nil, err
	}
	if !manifest.Valid {
		return nil, fmt.Errorf("map %q is MALFORMED: %s", m.ID, manifest.MalformedReason())
	}

	index := -1
	for i, entry := range manifest.Tickets {
		if entry.ID == ticketID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("map %q has no ticket %q", m.ID, ticketID)
	}
	entry := manifest.Tickets[index]
	ticket := entry.ToTicket()

	ticketPath := filepath.Join(m.Dir, issuesDirName, entry.File)
	content, err := d.FS.ReadFile(ticketPath)
	if err != nil {
		return nil, fmt.Errorf("read ticket %s: %w", ticketPath, err)
	}
	answered := ReplaceTicketAnswer(string(content), body)

	if err := writeMapFile(d, ticketPath, answered); err != nil {
		return nil, err
	}
	manifest.Tickets[index].Status = TicketResolved
	manifest.Tickets[index].OutOfScope = outOfScope
	manifest.Tickets[index].ADRDrafts = adrDrafts
	manifest.Tickets[index].ContextDrafts = contextDrafts
	if err := WriteMapManifest(d, manifest); err != nil {
		return nil, err
	}
	if err := renderMapIndex(d, m, manifest); err != nil {
		return nil, err
	}

	released, err := releaseTicketClaim(d, m.ID, ticketID)
	if err != nil {
		return nil, err
	}
	ticket.Status = TicketResolved
	ticket.OutOfScope = outOfScope
	ticket.ADRDrafts = adrDrafts
	ticket.ContextDrafts = contextDrafts
	return &ResolveResult{
		MapID:         m.ID,
		Ticket:        ticket,
		Path:          ticketPath,
		OutOfScope:    outOfScope,
		Replaced:      entry.Status == TicketResolved,
		ReleasedClaim: released,
	}, nil
}

// releaseTicketClaim hands a resolved ticket's claim back. A resolution is
// terminal, so the hold has nothing left to protect; leaving it would keep a
// dead grilling pane's name on the ticket until the TTL swept it.
func releaseTicketClaim(d *Deps, mapID, ticketID string) (string, error) {
	s, err := openWorkRegistry(d)
	if err != nil {
		return "", err
	}
	r := MapTicketRef(mapID, ticketID)
	claim, found, err := s.FindWorkClaim(r)
	if err != nil {
		return "", err
	}
	if err := s.ReleaseWorkItem(r); err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return claim.Owner, nil
}

// renderMapIndex rebuilds every generated region of map.md from the manifest and
// the answers on disk. It runs on every resolve rather than appending one line,
// because rebuilding is the only form of the write that two windows can perform
// in either order and still agree on the result.
func renderMapIndex(d *Deps, m Map, manifest *MapManifest) error {
	path := filepath.Join(m.Dir, mapFileName)
	content, err := d.FS.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	sections := []generatedSection{
		{
			name:    "decisions",
			heading: "Decisions so far",
			header:  decisionsSoFarHeader,
			body:    resolvedTicketLines(d, m.Dir, manifest, false),
		},
		{
			name:    "out-of-scope",
			heading: "Out of scope",
			header:  outOfScopeHeader,
			body:    resolvedTicketLines(d, m.Dir, manifest, true),
		},
		{
			name:    "spawned-sets",
			heading: "Spawned sets",
			header:  spawnedSetsHeader,
			body:    spawnedSetLines(manifest),
		},
	}
	return writeMapFile(d, path, renderGeneratedSections(string(content), sections))
}

// resolvedTicketLines renders one index section: every resolved ticket on the
// wanted side of the scope boundary, in manifest order, as a link plus the first
// line of its answer.
func resolvedTicketLines(d *Deps, mapDir string, manifest *MapManifest, outOfScope bool) []string {
	var lines []string
	for _, t := range manifest.ToTickets() {
		if t.Status != TicketResolved || t.OutOfScope != outOfScope {
			continue
		}
		line := fmt.Sprintf("- [%s](%s/%s)", ticketLinkLabel(t), issuesDirName, t.File)
		if gist := ticketAnswerGist(d, mapDir, t); gist != "" {
			line += " — " + gist
		}
		lines = append(lines, line)
	}
	return lines
}

func spawnedSetLines(manifest *MapManifest) []string {
	var lines []string
	for _, id := range manifest.SpawnedSets {
		lines = append(lines, "- "+id)
	}
	return lines
}

// ticketItemTitle is the label for a surface that already prints the ticket id
// in its own column. The link-label fallback (NN-slug) would repeat the id
// there, so this one drops the prefix and leaves just the slug.
func ticketItemTitle(t Ticket) string {
	if title := strings.TrimSpace(t.Title); title != "" {
		return title
	}
	if t.Slug != "" {
		return t.Slug
	}
	return t.ID
}

func ticketLinkLabel(t Ticket) string {
	if strings.TrimSpace(t.Title) != "" {
		return strings.TrimSpace(t.Title)
	}
	return ticketDisplayName(t)
}

// ticketAnswerGist reads the gist back off the ticket rather than remembering it
// from the resolve that wrote it: every rebuild then renders the same line,
// whichever window triggered it.
func ticketAnswerGist(d *Deps, mapDir string, t Ticket) string {
	content, err := d.FS.ReadFile(filepath.Join(mapDir, issuesDirName, t.File))
	if err != nil {
		return ""
	}
	return AnswerGist(ParseTicketAnswer(string(content)), decisionGistMaxLen)
}

func writeMapFile(d *Deps, path, content string) error {
	return tasks.WriteAtomicWith(&tasks.Deps{FS: d.FS}, path, []byte(content), 0o644)
}
