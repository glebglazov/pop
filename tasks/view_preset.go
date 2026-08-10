package tasks

import (
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

// ViewFacts is what a Work view preset evaluates against for one row
// (ADR-0197). Task-set and Map rows both project into this shape.
type ViewFacts struct {
	ID       string
	Status   string
	Archived bool
	Unfolded bool
	// MutedUntil is the container's Mute as stored, with no expiry applied. The
	// fact the preset asks about — "is this row muted?" — is only answerable
	// against an instant, and the instant belongs to the match, not to the row:
	// carrying the raw end here is what lets a mute lapse mid-session and the row
	// return on the next rebuild with nothing written (ADR-0200 decision 8).
	MutedUntil time.Time
}

// RowViewFacts projects a Task-set refresh row into ViewFacts.
func RowViewFacts(row Row) ViewFacts {
	return ViewFacts{
		ID:         row.ID,
		Status:     string(row.Status),
		Archived:   row.Archived,
		Unfolded:   Unfolded(row.Bound, row.Provisioned, row.Status),
		MutedUntil: row.MutedUntil,
	}
}

// MatchesPreset reports whether facts survive the preset: the positive filter
// AND-combines, then hide subtracts its match (ADR-0197). A zero preset (no
// name and no fields) behaves like an empty filter with archived=exclude.
func MatchesPreset(facts ViewFacts, preset config.WorkViewPreset, now time.Time) bool {
	if !matchesViewFilter(facts, preset.WorkViewPresetFilter, now) {
		return false
	}
	if preset.Hide != nil && matchesViewFilter(facts, *preset.Hide, now) {
		return false
	}
	return true
}

func matchesViewFilter(facts ViewFacts, filter config.WorkViewPresetFilter, now time.Time) bool {
	if len(filter.Status) > 0 {
		ok := false
		for _, want := range filter.Status {
			if strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(facts.Status)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if filter.Unfolded != nil && facts.Unfolded != *filter.Unfolded {
		return false
	}
	if filter.Muted != nil {
		if now.IsZero() {
			now = time.Now()
		}
		if facts.MutedUntil.After(now) != *filter.Muted {
			return false
		}
	}
	mode := filter.Archived
	if mode == "" {
		mode = config.ArchivedExclude
	}
	switch mode {
	case config.ArchivedExclude:
		if facts.Archived {
			return false
		}
	case config.ArchivedOnly:
		if !facts.Archived {
			return false
		}
	case config.ArchivedInclude:
		// both
	}
	if window := strings.TrimSpace(filter.CreatedWithin); window != "" {
		created, ok := IDCreatedAt(facts.ID)
		if !ok {
			return false
		}
		dur, err := config.ParsePresetDuration(window)
		if err != nil {
			return false
		}
		if now.IsZero() {
			now = time.Now()
		}
		if created.After(now) || now.Sub(created) > dur {
			return false
		}
	}
	return true
}

// IDCreatedAt reads the YYYY-MM-DD[-HHMM] prefix every Task-set and Map
// identifier carries. ok is false when the id has no parseable date — callers
// exclude rather than crash (ADR-0197).
func IDCreatedAt(id string) (time.Time, bool) {
	prefix := timestampPrefixPattern.FindString(id)
	if prefix == "" {
		return time.Time{}, false
	}
	prefix = strings.TrimSuffix(prefix, "-")
	switch len(prefix) {
	case len("2006-01-02"):
		t, err := time.ParseInLocation("2006-01-02", prefix, time.UTC)
		return t, err == nil
	case len("2006-01-02-1504"):
		t, err := time.ParseInLocation("2006-01-02-1504", prefix, time.UTC)
		return t, err == nil
	default:
		return time.Time{}, false
	}
}

// PresetWantsArchived reports whether the preset's archived mode requires the
// refresh that returns archived rows alongside active ones.
func PresetWantsArchived(preset config.WorkViewPreset) bool {
	mode := preset.Archived
	if mode == "" {
		mode = config.ArchivedExclude
	}
	return mode == config.ArchivedInclude || mode == config.ArchivedOnly
}
