package tasks

import (
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
)

// reviewComposition fingerprints the done-AFK work a review judges: the ids of
// the set's finished agent tasks, sorted so the fingerprint describes which work
// is done rather than the order the manifest happens to list it in.
//
// It is what a Review episode is keyed on, and the reason the episode needs no
// carve-outs. A commit that only moves the work SHA leaves the fingerprint
// alone, so a re-drain at unchanged work does not re-review; a task finishing —
// including a Remediation task the verify phase spawned — changes it, and the
// next quiescence reviews again. An empty fingerprint means the set has no
// finished agent work, which is nothing a review could judge.
func reviewComposition(m *Manifest) string {
	if m == nil {
		return ""
	}
	var ids []string
	for _, task := range m.Tasks {
		if task.Type == "AFK" && task.Status == TaskDone {
			ids = append(ids, task.ID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}

// reviewEpisodeArmed reports whether automatic review should fire for the set at
// this composition. Absence of a recorded episode is armed: a set nobody has
// reviewed has not been reviewed. Any store failure reads the same way, so the
// failure mode is a review that runs twice rather than a document that never
// gets written.
func reviewEpisodeArmed(d *Deps, repo, setID, composition string) bool {
	if strings.TrimSpace(composition) == "" {
		return false
	}
	if d == nil || repo == "" || setID == "" {
		return true
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return true
	}
	episode, err := s.GetReviewEpisode(repo, setID)
	if err != nil || episode == nil {
		return true
	}
	return episode.Composition != composition
}

// recordReviewEpisode disarms automatic review for the composition just judged.
// Every review records one — the drain's and the human's `pop tasks review`
// alike — because the rule is about the work having been reviewed, not about who
// asked for it.
//
// A failure to record is reported and otherwise ignored: the document is
// already written, and the only consequence is that the next quiescence reviews
// the same work again.
func recordReviewEpisode(d *Deps, out io.Writer, e store.ReviewEpisode) {
	if d == nil || e.Repo == "" || e.SetID == "" {
		return
	}
	s, err := openDrainStore(d)
	if err == nil {
		err = s.PutReviewEpisode(e)
	}
	if err != nil && out != nil {
		outputFor(out).line(ansiYellow, "   warning: record review episode for %s: %v", e.SetID, err)
	}
}

// reviewEpisodeRecord builds the row one completed review disarms with.
func reviewEpisodeRecord(repo, setID, workSHA, composition, document string, at time.Time) store.ReviewEpisode {
	return store.ReviewEpisode{
		Repo:        repo,
		SetID:       setID,
		WorkSHA:     workSHA,
		Composition: composition,
		Document:    document,
		ReviewedAt:  at.UTC(),
	}
}
