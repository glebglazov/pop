package supervisor

import (
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// bindSetInPlace binds setID to the repo checkout itself, so the set is
// drainable (ADR-0072) and routes in-place at the repo. With the
// integration-target fallback gone (ADR-0070), an unbound, no-directive set is
// no longer routed, so tests that exercise the spawn machinery on the repo
// checkout bind the set explicitly.
func bindSetInPlace(t *testing.T, d *drain.Deps, repo, setID string) {
	t.Helper()
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatalf("ResolveRepoKey: %v", err)
	}
	if err := binding.Put(d.Tasks, drain.SetScopedKey(repoKey, setID), drain.WorktreeBinding{RuntimePath: repo, Project: filepath.Base(repo)}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}
}

// taskSetAdvancer is the Task-set entry of the wiring list the supervisor drives.
func taskSetAdvancer(t *testing.T, d *drain.Deps, cfg *config.Config) work.Advancer {
	t.Helper()
	advs := work.Advancers(d.WorkKinds(cfg))
	if len(advs) == 0 {
		t.Fatal("no advancers wired")
	}
	return advs[0]
}

func taskSetCandidates(t *testing.T, d *drain.Deps, cfg *config.Config) []work.Candidate {
	t.Helper()
	candidates, err := taskSetAdvancer(t, d, cfg).Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	return candidates
}
