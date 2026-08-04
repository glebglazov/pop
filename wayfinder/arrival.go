package wayfinder

import (
	"fmt"
	"path/filepath"
)

// MapSessionName is the tmux session one Map's grilling panes live in. The
// naming is a shared fact rather than a private one: `arrive` tears the session
// down and `open` brings it back, so both ends have to agree on the name without
// consulting the other.
func MapSessionName(mapID string) string {
	return mapSessionPrefix + mapID
}

const mapSessionPrefix = "pop-map-"

// ArrivalResult is one declared change of a Map's lifecycle status.
type ArrivalResult struct {
	MapID    string
	Status   MapStatus
	Previous MapStatus
	// Unchanged reports that the Map already carried the target status. Both verbs
	// are idempotent — re-declaring arrival is not an error, it is a no-op with the
	// same outcome.
	Unchanged bool
	// Unfinished lists the tickets that were open or claimed at arrival. Arrival is
	// gated on the destination, not on an empty frontier, so these only ever warn.
	Unfinished []Ticket
	// KilledSession names the tmux session torn down, empty when the Map had none.
	KilledSession string
	// Session is the Map's session as `open` left it, nil for `arrive`.
	Session *MapSession
}

// ArriveMap declares a Map's destination reached: it writes `Status: arrived` and
// tears down the Map's tmux session. The gate is the destination, not empty fog —
// a Map may carry deliberately non-prerequisite fog forever, so a fog-empty gate
// would deadlock it. Open or claimed tickets are listed and the arrival proceeds:
// refusing would only buy fake resolutions typed to clear the gate.
func ArriveMap(d *Deps, cwd, mapID string) (*ArrivalResult, error) {
	result, err := setMapStatus(d, cwd, mapID, MapArrived)
	if err != nil {
		return nil, err
	}
	// The status is on disk before the session goes, so an `arrive` run from inside
	// the Map's own session — killing the process mid-verb — still leaves the Map
	// arrived rather than half-arrived.
	killed, err := killMapSession(d, result.MapID)
	if err != nil {
		return nil, err
	}
	result.KilledSession = killed
	return result, nil
}

// OpenMap reverses arrival: fog reopened, so the Map goes back to `active` and
// is grillable again, and the caller lands in the Map's tmux session. It never
// refuses a Map that is already active — `open` is also how you get back to a
// Map you never left, so the status write and the attach are independent halves
// of one verb.
func OpenMap(d *Deps, cwd, mapID string) (*ArrivalResult, error) {
	result, err := setMapStatus(d, cwd, mapID, MapActive)
	if err != nil {
		return nil, err
	}
	// The status is on disk before the attach, so an `open` that ends up blocked
	// on attach-session outside tmux has already reopened the Map.
	session, err := AttachMapSession(d, result.MapID)
	if err != nil {
		return nil, err
	}
	result.Session = session
	return result, nil
}

// setMapStatus rewrites map.md's Status: line under the Map's lock, so a status
// declaration and a concurrent resolve's re-render of the generated regions cannot
// interleave into a lost write.
func setMapStatus(d *Deps, cwd, mapID string, status MapStatus) (*ArrivalResult, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	result := &ArrivalResult{
		MapID:      m.ID,
		Status:     status,
		Previous:   m.Status,
		Unchanged:  m.Status == status,
		Unfinished: unfinishedTickets(m.Tickets),
	}
	if result.Unchanged {
		return result, nil
	}
	path := filepath.Join(m.Dir, mapFileName)
	err = withMapLock(d, m.ID, func() error {
		content, err := d.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return writeMapFile(d, path, ReplaceMapStatus(string(content), status))
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// unfinishedTickets is what arrival warns about: everything still open or held by
// a grilling pane when the human declared the destination reached.
func unfinishedTickets(tickets []Ticket) []Ticket {
	var out []Ticket
	for _, t := range tickets {
		if t.Status == TicketOpen || t.Status == TicketClaimed {
			out = append(out, t)
		}
	}
	return out
}

// killMapSession tears down the Map's tmux session if one is live. Arrival ends
// the grilling the session existed for; leaving it would leave windows pointed at
// a Map nobody is deciding anything about.
func killMapSession(d *Deps, mapID string) (string, error) {
	name := MapSessionName(mapID)
	t := d.tmux()
	if !t.HasSession(name) {
		return "", nil
	}
	if err := t.KillSession(name); err != nil {
		return "", fmt.Errorf("tear down tmux session %s: %w", name, err)
	}
	return name, nil
}
