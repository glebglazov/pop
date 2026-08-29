package tasks

import (
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
)

// refineComposition fingerprints the done-AFK work a refine pass judges: the ids
// of the set's finished agent tasks, minus its Remediation tasks, sorted so the
// fingerprint describes which work is done rather than the order the manifest
// happens to list it in.
//
// It is what a Refine episode is keyed on. A commit that only moves the work SHA
// leaves the fingerprint alone, so a re-drain at unchanged work does not
// re-refine; planned work finishing changes it, and the next quiescence refines
// again.
//
// Remediation tasks are the one carve-out (ADR-0240). Refine now runs before the
// Verifier, so a FIXABLE verdict spawns fix work whose completion would
// otherwise re-arm the episode and put a heavy refine pass inside every
// verify→remediate→re-verify lap — the iteration that must be cheapest. The
// price is that a remediation diff lands unrefined until real work re-arms the
// episode, which is the trade the ADR makes deliberately.
//
// An empty fingerprint means the set has no finished non-remediation agent work,
// which is nothing an automatic refine pass claims to judge.
func refineComposition(m *Manifest) string {
	if m == nil {
		return ""
	}
	var ids []string
	for _, task := range m.Tasks {
		if task.Type == "AFK" && task.Status == TaskDone && !remediationIDPattern.MatchString(task.ID) {
			ids = append(ids, task.ID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}

// refineEpisodeArmed reports whether automatic Refine should fire for the set at
// this composition. Absence of a recorded episode is armed: a set nobody has
// refined has not been refined. Any store failure reads the same way, so the
// failure mode is a refine pass that runs twice rather than a report that never
// gets written.
func refineEpisodeArmed(d *Deps, repo, setID, composition string) bool {
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
	episode, err := s.GetRefineEpisode(repo, setID)
	if err != nil || episode == nil {
		return true
	}
	return episode.Composition != composition
}

// recordRefineEpisode disarms automatic Refine for the composition just judged.
// Every refine pass records one — the drain's and the human's
// `pop tasks refine` alike — because the rule is about the work having been
// refined, not about who asked for it.
//
// A failure to record is reported and otherwise ignored: the report is already
// written, and the only consequence is that the next quiescence refines
// the same work again.
func recordRefineEpisode(d *Deps, out io.Writer, e store.RefineEpisode) {
	if d == nil || e.Repo == "" || e.SetID == "" {
		return
	}
	s, err := openDrainStore(d)
	if err == nil {
		err = s.PutRefineEpisode(e)
	}
	if err != nil && out != nil {
		outputFor(out).line(ansiYellow, "   warning: record refine episode for %s: %v", e.SetID, err)
	}
}

// refineEpisodeRecord builds the row one completed refine pass disarms with.
func refineEpisodeRecord(repo, setID, workSHA, composition, document string, at time.Time) store.RefineEpisode {
	return store.RefineEpisode{
		Repo:        repo,
		SetID:       setID,
		WorkSHA:     workSHA,
		Composition: composition,
		Document:    document,
		RefinedAt:   at.UTC(),
	}
}
