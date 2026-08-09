package wayfinder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work/ref"
)

// ErrFrontierEmpty reports that a Map has no ticket left to hand out: everything
// is resolved, blocked behind something unresolved, or held by a live claim. It
// is the ordinary end of parallel grilling, so `pop map next` exits nonzero with
// it rather than pretending success on no ticket.
var ErrFrontierEmpty = errors.New("frontier is empty")

// MapTicketRef names one Decision ticket as a Work item, the shape claims are
// keyed by.
func MapTicketRef(mapID, ticketID string) ref.WorkRef {
	return ref.WorkRef{Kind: ref.KindMap, ContainerID: mapID, ItemID: ticketID}
}

// DefaultClaimOwner is who a claim belongs to: the tmux pane the command runs in
// when there is one, else this process. A pane outlives the command that took the
// claim, which is what makes it the right identity for a grilling pane; the pid
// is the honest fallback for a claim taken from a plain shell. There is no
// configuration and no login — an owner is only ever compared for equality and
// probed for life.
func DefaultClaimOwner(t tmux.Tmux) string {
	return (&ownerLiveness{tmux: t, pidLive: pidAlive}).selfOwner()
}

// paneOwner is the one place a pane id becomes a claim owner, so the identity a
// spawned pane is claimed for and the one that pane computes for itself are the
// same string. The pane's pid rides along because tmux hands the same pane ids
// out again after a server restart, and with no TTL left a stale owner naming a
// reused id would wedge its ticket forever.
func paneOwner(paneID string, pid int) string {
	if pid <= 0 {
		return "pane:" + paneID
	}
	return fmt.Sprintf("pane:%s/%d", paneID, pid)
}

// selfOwner is what this process calls itself: its pane when it runs in one,
// else its pid. It hangs off ownerLiveness so the pane's pid comes from the
// listing that read is already making, not a second fork.
func (l *ownerLiveness) selfOwner() string {
	if pane := tmux.PaneIDFromEnv(); pane != "" {
		return l.paneOwner(pane)
	}
	return fmt.Sprintf("pid:%d", os.Getpid())
}

// ClaimResult is one taken claim: which ticket, where its markdown lives, and
// what the claim displaced.
type ClaimResult struct {
	MapID     string
	Ticket    Ticket
	Path      string
	Owner     string
	ClaimedAt time.Time
	// Reclaimed carries the dead owner's claim this one took over, nil when the
	// ticket was free. It is reported, never silent: the session that died may
	// have left half-written drafts on the ticket.
	Reclaimed *store.WorkClaim
	// UnresolvedBlockers names the blockers still open on an explicitly claimed
	// ticket. `next` never produces any — it hands out frontier tickets only —
	// but a human naming a ticket is allowed past the ordering, warned.
	UnresolvedBlockers []string
}

// ClaimTicketForPane claims one ticket for the pane now grilling it, in a single
// store transaction. It is the second half of the spawn path (ADR-0182): the pane
// exists before the claim does, so the claim lives exactly as long as the pane
// actually doing the work and that pane's own `pop map claim` re-claims rather
// than being refused.
//
// A live claim held elsewhere is not an error but a nil result: the ticket was on
// the frontier when the scan read it and went to a parallel session since, which
// the caller reports as one idle pane. A dead owner's claim is taken over and
// reported as a reclaim.
func ClaimTicketForPane(d *Deps, m Map, ticket Ticket, paneID string) (*ClaimResult, error) {
	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	// One liveness object serves both halves of the claim: the owner it records
	// and the owners it probes read the same pane listing, one fork.
	l := d.ownerLiveness()
	res, err := s.ClaimFirstWorkItem(MapRef(m.ID), []string{ticket.ID}, spawnedPaneOwner(d, l, paneID), d.now(), l.live)
	if errors.Is(err, store.ErrNoClaimableWorkItem) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return claimResult(m, res), nil
}

// spawnedPaneOwner names the pane a claim is taken for. A pane id pop could not
// read falls back to the caller's own identity, so a claim is never ownerless.
func spawnedPaneOwner(d *Deps, l *ownerLiveness, paneID string) string {
	if id := strings.TrimSpace(paneID); id != "" {
		return l.paneOwner(id)
	}
	return d.ownerWith(l)
}

// ClaimTicket claims one named ticket — the override for when the human, not
// manifest order, picks what to grill next. It refuses a ticket a live claim
// already holds, so naming a ticket cannot be used to walk over another window.
func ClaimTicket(d *Deps, cwd, mapID, rawTicket string) (*ClaimResult, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	ticketID, err := ticketIDFromArg(rawTicket)
	if err != nil {
		return nil, err
	}
	ticket, ok := findTicket(m.Tickets, ticketID)
	if !ok {
		return nil, fmt.Errorf("map %q has no ticket %q; valid: %s", m.ID, ticketID, ticketIDList(m.Tickets))
	}
	if ticket.Status == TicketResolved {
		return nil, fmt.Errorf("ticket %s of map %q is already resolved", ticket.ID, m.ID)
	}
	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	l := d.ownerLiveness()
	res, err := s.ClaimWorkItem(MapTicketRef(m.ID, ticket.ID), d.ownerWith(l), d.now(), l.live)
	var claimed *store.WorkItemClaimedError
	if errors.As(err, &claimed) {
		return nil, fmt.Errorf("ticket %s of map %q is claimed by %s since %s; it frees itself when that session ends",
			ticket.ID, m.ID, claimed.Claim.Owner,
			claimed.Claim.ClaimedAt.Format(time.RFC3339))
	}
	if err != nil {
		return nil, err
	}
	result := claimResult(m, res)
	result.UnresolvedBlockers = unresolvedBlockers(ticket, m.Tickets)
	return result, nil
}

func claimResult(m Map, res store.WorkClaimResult) *ClaimResult {
	ticket, _ := findTicket(m.Tickets, res.Claim.Ref.ItemID)
	return &ClaimResult{
		MapID:     m.ID,
		Ticket:    ticket,
		Path:      ticketPath(m, ticket),
		Owner:     res.Claim.Owner,
		ClaimedAt: res.Claim.ClaimedAt,
		Reclaimed: res.Reclaimed,
	}
}

// ticketPath is where the grilling session reads the question from — the whole
// point of printing a claim.
func ticketPath(m Map, t Ticket) string {
	name := t.File
	if name == "" {
		name = t.ID + ".md"
	}
	return filepath.Join(m.Dir, issuesDirName, name)
}

// findClaimableMap resolves the Map a claim verb acts on and refuses one that has
// not ended charting. Claims are rows against a Work container, so a Map that is
// not one yet has nothing to claim against; registration is also the gate that
// proved the manifest reads, and manifest order is what `next` walks.
func findClaimableMap(d *Deps, cwd, mapID string) (Map, error) {
	registered, err := registeredMapIDs(d)
	if err != nil {
		return Map{}, err
	}
	if strings.TrimSpace(mapID) == "" {
		return soleActiveMap(d, cwd, registered)
	}
	m, err := FindMap(d, cwd, mapID)
	if err != nil {
		return Map{}, err
	}
	if !registered[m.ID] {
		return Map{}, fmt.Errorf("map %q is not registered; run `pop map register %s` first", m.ID, m.ID)
	}
	if m.Broken {
		return Map{}, fmt.Errorf("map %q is BROKEN: %s", m.ID, m.BrokenReason)
	}
	return m, nil
}

// soleActiveMap is what `pop map next` with no argument means: the one Map this
// repository is currently wayfinding. Two of them is an ambiguity only the human
// can settle, so it lists them rather than guessing.
func soleActiveMap(d *Deps, cwd string, registered map[string]bool) (Map, error) {
	maps, err := ScanMaps(d, cwd)
	if err != nil {
		return Map{}, err
	}
	var candidates []Map
	for _, m := range maps {
		if m.Archived || m.Broken || m.Status != MapActive || !registered[m.ID] {
			continue
		}
		candidates = append(candidates, m)
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return Map{}, errors.New("no active registered map here; name one, or run `pop map register <map-id>`")
	default:
		ids := make([]string, len(candidates))
		for i, m := range candidates {
			ids[i] = m.ID
		}
		return Map{}, fmt.Errorf("several active maps here (%s); name the one to grill", strings.Join(ids, ", "))
	}
}

// registeredMapIDs reads the Work registry's Map rows. A machine with no pop.db
// has registered nothing, which reads as "run register first" rather than an
// error — the same way every other pure read treats a missing store.
func registeredMapIDs(d *Deps) (map[string]bool, error) {
	out := map[string]bool{}
	s, ok, err := d.taskDeps().Store(false)
	if err != nil || !ok {
		return out, err
	}
	rows, err := s.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.Ref.ContainerID] = true
	}
	return out, nil
}

// liveMapClaims returns every Map's live claims by map id and ticket id, for the
// scan that overlays them onto tickets read from disk. A claim whose grilling
// session has died is not among them, so its ticket is back on the frontier at
// this read — one liveness probe serves the whole scan.
func liveMapClaims(d *Deps) (map[string]map[string]store.WorkClaim, error) {
	out := map[string]map[string]store.WorkClaim{}
	s, ok, err := d.taskDeps().Store(false)
	if err != nil || !ok {
		return out, err
	}
	claims, err := s.LiveWorkClaimsOfKind(ref.KindMap, d.ownerLive())
	if err != nil {
		return nil, err
	}
	for _, claim := range claims {
		byTicket := out[claim.Ref.ContainerID]
		if byTicket == nil {
			byTicket = map[string]store.WorkClaim{}
			out[claim.Ref.ContainerID] = byTicket
		}
		byTicket[claim.Ref.ItemID] = claim
	}
	return out, nil
}

// applyClaims overlays pop.db's live claims onto tickets read from the manifest.
// Claimed is a derived status with no file behind it: the manifest says open or
// resolved, and this is where the third state comes from. A claim on a resolved
// ticket is ignored rather than reviving it — resolution is the durable fact.
func applyClaims(tickets []Ticket, claims map[string]store.WorkClaim) {
	if len(claims) == 0 {
		return
	}
	for i := range tickets {
		claim, ok := claims[tickets[i].ID]
		if !ok || tickets[i].Status != TicketOpen {
			continue
		}
		tickets[i].Status = TicketClaimed
		tickets[i].ClaimOwner = claim.Owner
		tickets[i].ClaimedAt = claim.ClaimedAt
	}
}

func emptyFrontier(m Map) error {
	return fmt.Errorf("map %q: %w — every Decision ticket is resolved, blocked, or claimed", m.ID, ErrFrontierEmpty)
}

func unresolvedBlockers(t Ticket, all []Ticket) []string {
	byID := make(map[string]TicketStatus, len(all))
	for _, other := range all {
		byID[other.ID] = other.Status
	}
	var out []string
	for _, blocker := range t.BlockedBy {
		if byID[blocker] != TicketResolved {
			out = append(out, blocker)
		}
	}
	return out
}

func findTicket(tickets []Ticket, id string) (Ticket, bool) {
	for _, t := range tickets {
		if t.ID == id {
			return t, true
		}
	}
	return Ticket{}, false
}

// ticketIDFromArg accepts the forms a human types for a ticket: "03", "3", and
// the filename or stem they just had open.
func ticketIDFromArg(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(filepath.Base(trimmed), ".md")
	if dash := strings.Index(trimmed, "-"); dash > 0 {
		trimmed = trimmed[:dash]
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 0 {
		return "", fmt.Errorf("invalid ticket %q: expected a ticket number like 03", raw)
	}
	return normalizeTicketID(strconv.Itoa(n)), nil
}

func ticketIDList(tickets []Ticket) string {
	if len(tickets) == 0 {
		return "(none)"
	}
	ids := make([]string, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}
