package tasks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
)

// remediationSet is a set with the given number of Remediation tasks already
// present, plus one baseline AFK task. It exercises the depth counter.
func remediationSet(remediationCount int) []Task {
	tasks := []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}}
	for i := 0; i < remediationCount; i++ {
		id := fmt.Sprintf("%02d-remediation", i+2)
		tasks = append(tasks, Task{ID: id, File: id + ".md", Title: "Remediation", Type: "AFK", Status: "done"})
	}
	return tasks
}

func TestRemediationDepth(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		_, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), remediationSet(count), nil)
		if got := remediationDepth(m); got != count {
			t.Fatalf("remediationDepth() = %d, want %d", got, count)
		}
	}
}

func TestNextTaskNumber(t *testing.T) {
	_, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "open"},
	}, nil)
	if got := nextTaskNumber(m); got != 4 {
		t.Fatalf("nextTaskNumber() = %d, want 4 (one past the highest)", got)
	}
}

// TestSpawnRemediationTaskWritesMarkdownAndIndex: creating a Remediation task
// writes both a markdown body carrying the findings and an index.json entry at
// the next number, and the two stay in sync (the reloaded manifest is valid and
// references the file that was written).
func TestSpawnRemediationTaskWritesMarkdownAndIndex(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)

	id, err := spawnRemediationTask(d, m, "", "deadbeefcafe", "criterion 2 unmet: the widget never renders", "")
	if err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}
	if id != "02-remediation" {
		t.Fatalf("id = %q, want 02-remediation", id)
	}

	// Markdown body: findings verbatim, an Acceptance criteria section with a
	// checkbox (so the manifest validator accepts it), and the work SHA.
	mdPath := filepath.Join(m.Dir, "02-remediation.md")
	body, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read remediation markdown: %v", err)
	}
	for _, want := range []string{"criterion 2 unmet: the widget never renders", "## Acceptance criteria", "- [ ]", "deadbeefcafe"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("remediation body missing %q:\n%s", want, body)
		}
	}

	// index.json and markdown stay in sync: the reloaded manifest is valid and
	// carries the new AFK/open task pointing at the file just written.
	reloaded := LoadManifest(d, "demo", m.Path)
	if !reloaded.Valid {
		t.Fatalf("reloaded manifest invalid: %v", reloaded.Errors)
	}
	var rem *Task
	for i := range reloaded.Tasks {
		if reloaded.Tasks[i].ID == id {
			rem = &reloaded.Tasks[i]
		}
	}
	if rem == nil {
		t.Fatalf("reloaded manifest missing task %q", id)
	}
	if rem.File != "02-remediation.md" || rem.Type != "AFK" || rem.Status != "open" {
		t.Fatalf("remediation task = %+v, want AFK/open file 02-remediation.md", rem)
	}
	if !strings.Contains(rem.Title, "Remediation") {
		t.Fatalf("title = %q, want a Remediation title", rem.Title)
	}
}

// TestSpawnRemediationTaskFindingsNotWrittenIntoOtherSpecs: the baseline task's
// spec is never touched — findings live only in the new Remediation task's body.
func TestSpawnRemediationTaskFindingsNotWrittenIntoOtherSpecs(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	before, err := os.ReadFile(filepath.Join(m.Dir, "01-a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawnRemediationTask(d, m, "", "sha1", "some finding", ""); err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(m.Dir, "01-a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("baseline task spec was modified:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSpawnRemediationTaskFindingsWithACHeaderStaysValid: findings that echo a
// literal "## Acceptance criteria" heading must not produce a second section
// that fails manifest validation — the task must still load valid.
func TestSpawnRemediationTaskFindingsWithACHeaderStaysValid(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	findings := "The set does not meet:\n## Acceptance criteria\n- the second box is unchecked"
	if _, err := spawnRemediationTask(d, m, "", "sha1", findings, ""); err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}
	reloaded := LoadManifest(d, "demo", m.Path)
	if !reloaded.Valid {
		t.Fatalf("remediation task with an AC-header in findings produced an invalid manifest: %v", reloaded.Errors)
	}
	// The finding text is preserved, just demoted so it no longer parses as a heading.
	body, err := os.ReadFile(filepath.Join(m.Dir, "02-remediation.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "second box is unchecked") {
		t.Fatalf("finding text lost:\n%s", body)
	}
}

// TestSpawnRemediationIfUnderCap: under the cap each call spawns one task at the
// next number; at the cap nothing is written.
func TestSpawnRemediationIfUnderCap(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)

	spawned, id, err := spawnRemediationIfUnderCap(d, m, "", "sha1", "f1", 2)
	if err != nil || !spawned {
		t.Fatalf("first spawn: spawned=%v err=%v, want true/nil", spawned, err)
	}
	if id != "02-remediation" {
		t.Fatalf("first spawn id = %q, want 02-remediation", id)
	}
	if remediationDepth(m) != 1 {
		t.Fatalf("depth after first spawn = %d, want 1", remediationDepth(m))
	}

	spawned, id, err = spawnRemediationIfUnderCap(d, m, "", "sha1", "f2", 2)
	if err != nil || !spawned {
		t.Fatalf("second spawn: spawned=%v err=%v, want true/nil", spawned, err)
	}
	if id != "03-remediation" {
		t.Fatalf("second spawn id = %q, want 03-remediation", id)
	}
	if remediationDepth(m) != 2 {
		t.Fatalf("depth after second spawn = %d, want 2", remediationDepth(m))
	}

	spawned, _, err = spawnRemediationIfUnderCap(d, m, "", "sha1", "f3", 2)
	if err != nil {
		t.Fatalf("third spawn err = %v", err)
	}
	if spawned {
		t.Fatal("third spawn returned spawned=true despite reaching the cap")
	}
	if remediationDepth(m) != 2 {
		t.Fatalf("depth after capped spawn = %d, want 2 (unchanged)", remediationDepth(m))
	}
}

// TestSpawnRemediationCapZeroNeverSpawns: a cap of 0 disables remediation
// entirely — a FIXABLE verdict spawns nothing.
func TestSpawnRemediationCapZeroNeverSpawns(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	spawned, _, err := spawnRemediationIfUnderCap(d, m, "", "sha1", "f", 0)
	if err != nil {
		t.Fatalf("spawn err = %v", err)
	}
	if spawned || remediationDepth(m) != 0 {
		t.Fatalf("cap 0 spawned=%v depth=%d, want false/0", spawned, remediationDepth(m))
	}
}

func TestMaxRemediationDepth(t *testing.T) {
	if got := maxRemediationDepth(nil); got != DefaultMaxRemediationDepth {
		t.Fatalf("nil config = %d, want default %d", got, DefaultMaxRemediationDepth)
	}
	if got := maxRemediationDepth(verifyEnabledConfig()); got != DefaultMaxRemediationDepth {
		t.Fatalf("unset cap = %d, want default %d", got, DefaultMaxRemediationDepth)
	}
	five := 5
	cfg := verifyEnabledConfig()
	cfg.Task.Verify.MaxRemediationDepth = &five
	if got := maxRemediationDepth(cfg); got != 5 {
		t.Fatalf("configured cap = %d, want 5", got)
	}
}

// workSHAFromPrompt extracts the "Work SHA:" line the Verifier prompt carries,
// so a test can confirm a re-verify ran at a fresh SHA.
func workSHAFromPrompt(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "Work SHA:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Work SHA:"))
		}
	}
	return ""
}

// TestRunTaskSetFixableRemediationLoopReVerifies: a FIXABLE verdict spawns a
// Remediation task the Drain then drains; its completion moves the work SHA, so
// the Verifier re-fires at the new SHA. On the re-verify PASS the set reaches
// DONE. End-to-end through RunTaskSetWith with an injected Verifier.
func TestRunTaskSetFixableRemediationLoopReVerifies(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	// Each task appends to a tracked file, so every completed task moves HEAD and
	// each re-verify runs at a fresh work SHA (cache miss).
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	var shas []string
	calls := 0
	verify := func(prompt string) (string, error) {
		calls++
		shas = append(shas, workSHAFromPrompt(prompt))
		if calls == 1 {
			return "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet\n", nil
		}
		return "VERDICT: PASS\n", nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = verify

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone after remediation + PASS", result)
	}
	if calls != 2 {
		t.Fatalf("verifier calls = %d, want 2 (initial FIXABLE, then re-verify)", calls)
	}
	if len(shas) == 2 && shas[0] == shas[1] {
		t.Fatalf("re-verify ran at the same SHA %q; want a fresh SHA after remediation moved HEAD", shas[0])
	}

	// The Remediation task was created (AFK) and drained to done, and its body
	// carries the findings.
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
	var rem *Task
	for i := range m.Tasks {
		if m.Tasks[i].ID == "02-remediation" {
			rem = &m.Tasks[i]
		}
	}
	if rem == nil {
		t.Fatal("no Remediation task was created")
	}
	if rem.Type != "AFK" || rem.Status != "done" {
		t.Fatalf("remediation task = %+v, want AFK/done", rem)
	}
	body, err := os.ReadFile(filepath.Join(m.Dir, rem.File))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "criterion 2 unmet") {
		t.Fatalf("remediation body missing findings:\n%s", body)
	}
}

// TestRunTaskSetRemediationDepthCapParks: a persistently FIXABLE set spawns
// Remediation tasks up to the per-set cap, then parks at VERIFY-FAILED and
// records the verify_failed drain terminal — no further task is spawned.
func TestRunTaskSetRemediationDepthCapParks(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	depthCap := 1
	cfg := verifyEnabledConfig()
	cfg.Task.Verify.MaxRemediationDepth = &depthCap

	verify := func(string) (string, error) { return "VERDICT: FIXABLE\nFINDINGS: still failing\n", nil }

	_, runtimePath, _ := runtimeHead(t, d, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = verify

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return cfg, nil
	}, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if result == nil || !result.TaskSetVerifyFailed {
		t.Fatalf("result = %+v, want TaskSetVerifyFailed", result)
	}
	if result.TaskSetDone {
		t.Fatal("a capped-out FIXABLE set must not reach DONE")
	}

	// Exactly one Remediation task was spawned before the cap parked the set.
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
	if got := remediationDepth(m); got != depthCap {
		t.Fatalf("remediation depth = %d, want %d (capped)", got, depthCap)
	}

	// The drain recorded the verify_failed terminal, not a bare finished.
	rec := latestTerminalDrain(t, d, runtimePath)
	if rec == nil {
		t.Fatal("no terminal drain recorded")
	}
	if rec.State != store.StateVerifyFailed {
		t.Fatalf("outcome = %q, want %q", rec.State, store.StateVerifyFailed)
	}
}
