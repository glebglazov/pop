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
// claim, which is what makes it the right identity for a grilling window; the pid
// is the honest fallback for a claim taken from a plain shell. There is no
// configuration and no login — an owner is only ever compared for equality.
func DefaultClaimOwner() string {
	if pane := tmux.PaneIDFromEnv(); pane != "" {
		return "pane:" + pane
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
	// Stole carries the expired claim this one took over, nil when the ticket was
	// free. A steal is reported, never silent: the window that lost the ticket may
	// still be looking at it.
	Stole *store.WorkClaim
	// UnresolvedBlockers names the blockers still open on an explicitly claimed
	// ticket. `next` never produces any — it hands out frontier tickets only —
	// but a human naming a ticket is allowed past the ordering, warned.
	UnresolvedBlockers []string
}

// NextTicket claims the first frontier ticket of a Map in manifest order. The
// pick and the claim are one store transaction, so two grilling windows racing
// the same Map get two different tickets: the loser of the write lock sees the
// winner's row and moves on to the next candidate.
func NextTicket(d *Deps, cwd, mapID string) (*ClaimResult, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	frontier := Frontier(m.Tickets)
	if len(frontier) == 0 {
		return nil, emptyFrontier(m)
	}
	candidates := make([]string, 0, len(frontier))
	for _, t := range frontier {
		candidates = append(candidates, t.ID)
	}
	s, err := openWorkRegistry(d)
	if err != nil {
		return nil, err
	}
	res, err := s.ClaimFirstWorkItem(MapRef(m.ID), candidates, d.owner(), d.now())
	if errors.Is(err, store.ErrNoClaimableWorkItem) {
		// Every candidate went to a concurrent window between the scan and the
		// transaction. That is the same answer as an empty frontier.
		return nil, emptyFrontier(m)
	}
	if err != nil {
		return nil, err
	}
	return claimResult(m, res), nil
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
	res, err := s.ClaimWorkItem(MapTicketRef(m.ID, ticket.ID), d.owner(), d.now())
	var claimed *store.WorkItemClaimedError
	if errors.As(err, &claimed) {
		return nil, fmt.Errorf("ticket %s of map %q is claimed by %s since %s; it frees itself after %s",
			ticket.ID, m.ID, claimed.Claim.Owner,
			claimed.Claim.ClaimedAt.Format(time.RFC3339), store.WorkClaimTTL)
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
		Stole:     res.Stole,
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
	if m.Malformed {
		return Map{}, fmt.Errorf("map %q is MALFORMED: %s", m.ID, m.MalformedReason)
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
		if m.Archived || m.Malformed || m.Status != MapActive || !registered[m.ID] {
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
// scan that overlays them onto tickets read from disk.
func liveMapClaims(d *Deps) (map[string]map[string]store.WorkClaim, error) {
	out := map[string]map[string]store.WorkClaim{}
	s, ok, err := d.taskDeps().Store(false)
	if err != nil || !ok {
		return out, err
	}
	claims, err := s.LiveWorkClaimsOfKind(ref.KindMap, d.now())
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
