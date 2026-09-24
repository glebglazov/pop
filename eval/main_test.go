package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
)

func TestEvalWorkRootUsesXDGStateHome(t *testing.T) {
	got := evalWorkRootWith(func(key string) string {
		if key == "XDG_STATE_HOME" {
			return "/state"
		}
		return ""
	}, func() (string, error) {
		return "", errors.New("home must not be read")
	})

	want := filepath.Join("/state", "pop", "eval", "work")
	if got != want {
		t.Fatalf("Eval work root = %q, want %q", got, want)
	}
}

func TestEvalWorkRootUsesHomeFallback(t *testing.T) {
	got := evalWorkRootWith(func(string) string { return "" }, func() (string, error) {
		return "/home/evaluator", nil
	})

	want := filepath.Join("/home/evaluator", ".local", "state", "pop", "eval", "work")
	if got != want {
		t.Fatalf("Eval work root = %q, want %q", got, want)
	}
}

func TestCleanEvalWork(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"trial-one", "example-acceptance-two", "prepare-three", "unrelated"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, dir, "payload"), strings.Repeat("x", 32))
	}
	var out bytes.Buffer
	if err := cleanEvalWork(root, &out); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"trial-one", "example-acceptance-two", "prepare-three"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			t.Fatalf("disposable work directory %s remains: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "unrelated")); err != nil {
		t.Fatalf("unrelated work directory was removed: %v", err)
	}
	if !strings.Contains(out.String(), "Cleaned 3 Eval work directories") || !strings.Contains(out.String(), "reclaimed ") || strings.Contains(out.String(), "reclaimed 0 bytes") {
		t.Fatalf("clean report = %q", out.String())
	}
}

func TestCleanEvalWorkEmptyOrAbsent(t *testing.T) {
	root := t.TempDir()
	for name, tc := range map[string]struct{ path, message string }{
		"empty":  {root, "no disposable work"},
		"absent": {filepath.Join(root, "absent"), "is absent"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := cleanEvalWork(tc.path, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.message) || !strings.Contains(out.String(), "reclaimed 0 bytes") {
				t.Fatalf("clean report = %q", out.String())
			}
		})
	}
}

func TestPrepareRefusesAReferenceRangeWithOtherWork(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	runGit(t, repo, "remote", "add", "origin", "https://example.test/project.git")
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.test/project\n\ngo 1.23\n")
	writeFile(t, filepath.Join(repo, "doc.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "initial")

	setID := "2026-09-08-fixture"
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	identity, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	setDir := filepath.Join(identity.TasksDir, setID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(setDir, tasks.ManifestFileName), `{
  "tasks": [
    {"id":"01-build","file":"01-build.md","title":"Build it","type":"AFK","status":"done","blocked_by":[]},
    {"id":"02-check","file":"02-check.md","title":"Check it","type":"AFK","status":"done","blocked_by":["01-build"]}
  ]
}`)
	writeFile(t, filepath.Join(setDir, "01-build.md"), taskBody("Build it", "First behaviour"))
	writeFile(t, filepath.Join(setDir, "02-check.md"), taskBody("Check it", "Second behaviour"))

	writeFile(t, filepath.Join(repo, "one.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "build\n\nPop-Task: "+setID+"/01-build")
	writeFile(t, filepath.Join(repo, "other.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "work of another set\n\nPop-Task: 2026-09-08-other/01-other")
	writeFile(t, filepath.Join(repo, "two.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "check\n\nPop-Task: "+setID+"/02-check")

	outputRoot := filepath.Join(t.TempDir(), "cases")
	_, err = prepareCase(prepareOptions{repositoryPath: repo, setID: setID, outputRoot: outputRoot, work: filepath.Join(t.TempDir(), "work")})
	if err == nil || !strings.Contains(err.Error(), "work of another set") {
		t.Fatalf("prepare with another set's commit in the Reference range: err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outputRoot, setID)); !os.IsNotExist(statErr) {
		t.Fatalf("Case written despite other work in its Reference range: %v", statErr)
	}
}

func TestPrepareCaseFromHistoricalTaskSet(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	runGit(t, repo, "remote", "add", "origin", "https://example.test/project.git")
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.test/project\n\ngo 1.23\n")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "# Standards\n")
	// The default gates build and test the parent, which needs one package.
	writeFile(t, filepath.Join(repo, "doc.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "initial")
	parent := runGit(t, repo, "rev-parse", "HEAD")

	setID := "2026-09-08-fixture"
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	d := tasks.DefaultDeps()
	identity, err := tasks.ResolveRepositoryIdentity(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	setDir := filepath.Join(identity.TasksDir, setID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "verify": false,
  "base_commit": "historical",
  "human_completed": true,
  "tasks": [
    {"id":"01-build","file":"01-build.md","title":"Build it","type":"AFK","status":"done","blocked_by":[],"commit":{"sha":"old","subject":"old"}},
    {"id":"02-check","file":"02-check.md","title":"Check it","type":"AFK","status":"failed","blocked_by":["01-build","03-sign-off"],"failed_after":2},
    {"id":"03-sign-off","file":"03-sign-off.md","title":"Sign off","type":"HITL","status":"open","blocked_by":["02-check"]},
    {"id":"04-remediation","file":"04-remediation.md","title":"Remediation 1: resolve verification findings","type":"AFK","status":"done","blocked_by":[]}
  ]
}`
	writeFile(t, filepath.Join(setDir, tasks.ManifestFileName), manifest)
	writeFile(t, filepath.Join(setDir, "01-build.md"), taskBody("Build it", "First behaviour"))
	writeFile(t, filepath.Join(setDir, "02-check.md"), taskBody("Check it", "Second behaviour"))
	writeFile(t, filepath.Join(setDir, "03-sign-off.md"), taskBody("Sign off", "Human approval"))
	writeFile(t, filepath.Join(setDir, "04-remediation.md"), taskBody("Resolve the findings", "Verifier finding"))

	writeFile(t, filepath.Join(repo, "one.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "build\n\nPop-Task: "+setID+"/01-build")
	writeFile(t, filepath.Join(repo, "two.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "check\n\nPop-Task: "+setID+"/02-check")
	writeFile(t, filepath.Join(repo, "refined.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "refine\n\nPop-Refine: "+setID)
	last := runGit(t, repo, "rev-parse", "HEAD")

	outputRoot := filepath.Join(t.TempDir(), "cases")
	work := filepath.Join(t.TempDir(), "work")
	// one.go first appears in the Task set's own commits, so this gate passes on
	// the final tree and fails only on the parent commit the Case starts from.
	_, err = prepareCase(prepareOptions{repositoryPath: repo, setID: setID, outputRoot: outputRoot, work: work, gates: []string{"test -f one.go"}})
	if err == nil || !strings.Contains(err.Error(), "test -f one.go") || !strings.Contains(err.Error(), "--gate") {
		t.Fatalf("prepare with a gate the parent fails: err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outputRoot, setID)); !os.IsNotExist(statErr) {
		t.Fatalf("Case written despite a failing baseline gate: %v", statErr)
	}
	caseDir, err := prepareCase(prepareOptions{repositoryPath: repo, setID: setID, outputRoot: outputRoot, work: work})
	if err != nil {
		t.Fatal(err)
	}
	if caseDir != filepath.Join(outputRoot, setID) {
		t.Fatalf("Case path = %q", caseDir)
	}

	var gotCase caseManifest
	decodeJSONFile(t, filepath.Join(caseDir, caseManifestName), &gotCase)
	if gotCase.RepositoryURL != "https://example.test/project.git" || gotCase.ParentCommit != parent {
		t.Fatalf("Case manifest = %#v", gotCase)
	}
	if gotCase.ReferenceRange != parent+".."+last {
		t.Fatalf("reference range = %q", gotCase.ReferenceRange)
	}
	if strings.Join(gotCase.GateCommands, "|") != "go build ./...|go test ./..." {
		t.Fatalf("gate commands = %#v", gotCase.GateCommands)
	}
	if strings.Join(gotCase.StandardDocuments, "|") != "AGENTS.md" {
		t.Fatalf("standard documents = %#v", gotCase.StandardDocuments)
	}

	var gotTasks struct {
		Verify *bool        `json:"verify"`
		Tasks  []tasks.Task `json:"tasks"`
	}
	decodeJSONFile(t, filepath.Join(caseDir, "tasks", tasks.ManifestFileName), &gotTasks)
	if gotTasks.Verify == nil || *gotTasks.Verify || len(gotTasks.Tasks) != 2 {
		t.Fatalf("stripped manifest = %#v", gotTasks)
	}
	for _, task := range gotTasks.Tasks {
		if task.Type == "HITL" || task.Status != tasks.TaskOpen || task.Commit != nil || task.FailedAfter != nil {
			t.Fatalf("task was not reset: %#v", task)
		}
		for _, blocker := range task.BlockedBy {
			if blocker == "03-sign-off" {
				t.Fatalf("stripped blocker remains: %#v", task)
			}
		}
	}
	preparedSet := tasks.LoadManifest(tasks.DefaultDeps(), setID, filepath.Join(caseDir, "tasks", tasks.ManifestFileName))
	if !preparedSet.Valid {
		t.Fatalf("prepared Task set is not valid: %v", preparedSet.Errors)
	}
	for _, file := range []string{"03-sign-off.md", "04-remediation.md"} {
		if _, err := os.Stat(filepath.Join(caseDir, "tasks", file)); !os.IsNotExist(err) {
			t.Fatalf("stripped task file %s exists: %v", file, err)
		}
	}
	spec := readFile(t, filepath.Join(caseDir, tasks.SpecFileName))
	if !strings.Contains(spec, "First behaviour") || !strings.Contains(spec, "Second behaviour") || strings.Contains(spec, "Human approval") || strings.Contains(spec, "Verifier finding") {
		t.Fatalf("spec content:\n%s", spec)
	}
	acceptance := readFile(t, filepath.Join(caseDir, acceptanceName))
	if !strings.Contains(acceptance, "Status: not approved") || !strings.Contains(acceptance, "- [ ] First behaviour") || !strings.Contains(acceptance, "- [ ] Second behaviour") || strings.Contains(acceptance, "Human approval") || strings.Contains(acceptance, "Verifier finding") {
		t.Fatalf("acceptance content:\n%s", acceptance)
	}

	if err := filepath.Walk(caseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content := readFile(t, path)
		if strings.Contains(content, repo) || strings.Contains(content, setDir) {
			t.Fatalf("%s contains a preparing-machine path", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDraftAcceptanceWritesBehavioursAndRecordsSpend(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "before\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "feature.txt"), "after\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "reference")
	reference := runGit(t, repo, "rev-parse", "HEAD")

	caseDir := filepath.Join(t.TempDir(), "sample-case")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := caseManifest{Name: "sample-case", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + ".." + reference}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(manifestData))
	writeFile(t, filepath.Join(caseDir, tasks.SpecFileName), "The command reports the new state.\n")
	writeFile(t, filepath.Join(caseDir, acceptanceName), "# Acceptance list\n\nStatus: not approved\n\n## Lifted criteria\n\n- [ ] The new state is reported\n")

	binDir := t.TempDir()
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	agent := `#!/bin/sh
for argument in "$@"; do
  prompt="$argument"
done
case "$prompt" in
  "Read the file "*)
    prompt_file=${prompt#Read the file }
    prompt_file=${prompt_file%% in full:*}
    cp "$prompt_file" "$FAKE_DRAFT_PROMPT"
    ;;
  *) printf '%s' "$prompt" > "$FAKE_DRAFT_PROMPT" ;;
esac
printf '%s\n' '{"type":"system","subtype":"init","model":"claude-test"}'
printf '%s\n' '{"type":"assistant","message":{"usage":{"input_tokens":123,"output_tokens":45}}}'
printf '%s\n' '{"type":"result","subtype":"success","result":"1. The command reports the new state.\n2. Existing output remains available.","usage":{"input_tokens":123,"output_tokens":45}}'
`
	writeFile(t, filepath.Join(binDir, "claude"), agent)
	if err := os.Chmod(filepath.Join(binDir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DRAFT_PROMPT", promptPath)

	path, err := draftAcceptance(draftAcceptanceOptions{
		casePath: caseDir, casesRoot: t.TempDir(), workRoot: filepath.Join(t.TempDir(), "work"),
		agentSpec: "claude --model claude-test", timeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(caseDir, acceptanceName) {
		t.Fatalf("Acceptance list path = %q", path)
	}
	want := "# Acceptance list\n\nStatus: draft\n\n1. The command reports the new state.\n2. Existing output remains available.\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("Acceptance list:\n%s\nwant:\n%s", got, want)
	}
	prompt := readFile(t, promptPath)
	for _, text := range []string{
		"observable behaviours only", "Never record code shape", "checkable against a diff",
		"Never record that the build, the vet or the tests pass",
		"The command reports the new state.", "- [ ] The new state is reported",
		"diff --git a/feature.txt b/feature.txt", "+after",
	} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("drafting prompt does not contain %q:\n%s", text, prompt)
		}
	}

	runs, err := os.ReadDir(filepath.Join(caseDir, draftingDirName, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("Captured run files = %v", runs)
	}
	var record capturedRunRecord
	decodeJSONFile(t, filepath.Join(caseDir, draftingDirName, draftingRecordName), &record)
	if record.Agent != "claude --model claude-test" || record.RunID == "" || record.Outcome != "completed" || record.Spend.Tokens.Input != 123 || !record.Spend.Tokens.HasInput {
		t.Fatalf("drafting record = %+v", record)
	}
}

func TestLoadApprovedAcceptanceRefusesDraftAndNamesFile(t *testing.T) {
	caseDir := t.TempDir()
	_, _, grader := testGrader(t, t.TempDir())
	manifest, _ := json.Marshal(caseManifest{Name: "example", RepositoryURL: "https://example.test/repo.git", ParentCommit: "abc", ReferenceRange: "abc..def"})
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(manifest))
	path := filepath.Join(caseDir, acceptanceName)
	writeFile(t, path, "# Acceptance list\n\nStatus: draft\n\n1. A behaviour.\n")
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "Status: approved") {
		t.Fatalf("draft approval error = %v", err)
	}
	approveCase(t, caseDir, "# Acceptance list\n\nStatus: approved\n\n1. A behaviour.\n", grader)
	if got, err := loadApprovedAcceptance(caseDir, grader); err != nil || !strings.Contains(got, "1. A behaviour.") {
		t.Fatalf("approved Acceptance list = %q, %v", got, err)
	}
}

func TestLoadApprovedAcceptanceHoldsTheListToItsReferenceCheck(t *testing.T) {
	caseDir := t.TempDir()
	_, _, grader := testGrader(t, t.TempDir())
	manifest, _ := json.Marshal(caseManifest{Name: "example", RepositoryURL: "https://example.test/repo.git", ParentCommit: "abc", ReferenceRange: "abc..def"})
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(manifest))
	list := "# Acceptance list\n\nStatus: approved\n\n1. A behaviour.\n2. Another behaviour.\n"
	writeFile(t, filepath.Join(caseDir, acceptanceName), list)
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "no Reference check") || !strings.Contains(err.Error(), "reference-check example") {
		t.Fatalf("missing check error = %v", err)
	}
	approveCase(t, caseDir, list, grader)
	checkPath := filepath.Join(caseDir, referenceCheckDirName, referenceCheckRecordName)
	var check referenceCheck
	decodeJSONFile(t, checkPath, &check)

	// A status or heading edit is not a list change; a behaviour edit is.
	writeFile(t, filepath.Join(caseDir, acceptanceName), strings.Replace(list, "# Acceptance list", "# Acceptance list, reviewed", 1))
	if _, err := loadApprovedAcceptance(caseDir, grader); err != nil {
		t.Fatalf("heading edit made the check stale: %v", err)
	}
	writeFile(t, filepath.Join(caseDir, acceptanceName), strings.Replace(list, "Another behaviour.", "Another behaviour in a whole-set drain.", 1))
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("list edit error = %v", err)
	}
	writeFile(t, filepath.Join(caseDir, acceptanceName), list)
	other := grader
	other.Model = "other-grader"
	if _, err := loadApprovedAcceptance(caseDir, other); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Grader change error = %v", err)
	}

	check.Grade.Scores.Items[1] = itemGrade{Item: 2, Met: false, Reason: "The Reference prints it only in a drain."}
	data, _ := json.Marshal(check)
	writeFile(t, checkPath, string(data))
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "items [2]") {
		t.Fatalf("unaccepted failure error = %v", err)
	}
	// A reason recorded for another behaviour at the same number does not count.
	check.Accepted = []acceptedFailure{{Item: 2, Behaviour: "A behaviour.", Reason: "Kept on purpose."}}
	data, _ = json.Marshal(check)
	writeFile(t, checkPath, string(data))
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "items [2]") {
		t.Fatalf("mismatched reason error = %v", err)
	}
	check.Accepted = []acceptedFailure{{Item: 2, Behaviour: "Another behaviour.", Reason: "The spec asks for it; the Reference missed it."}}
	data, _ = json.Marshal(check)
	writeFile(t, checkPath, string(data))
	if _, err := loadApprovedAcceptance(caseDir, grader); err != nil {
		t.Fatalf("accepted failure refused: %v", err)
	}

	check.Grade = &gradeRecord{Status: "gate_failed", Scores: &gradeScores{}}
	data, _ = json.Marshal(check)
	writeFile(t, checkPath, string(data))
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "grade=gate_failed") {
		t.Fatalf("gate failure error = %v", err)
	}
}

func taskBody(title, criterion string) string {
	return "## What to build\n\n" + title + "\n\n## Acceptance criteria\n\n- [x] " + criterion + "\n\n## Blocked by\n\n- None\n"
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// testGrader writes a Grader arm and a harness config under root that name it.
// Its model is one no test Arm uses.
func testGrader(t *testing.T, root string) (graders, config string, grader armFile) {
	t.Helper()
	graders, config = filepath.Join(root, "graders"), filepath.Join(root, "config.toml")
	if err := os.MkdirAll(graders, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(graders, "grader.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'grader-test'\n")
	writeFile(t, config, "grader_arm = 'grader'\n")
	grader, err := loadGrader(config, graders)
	if err != nil {
		t.Fatal(err)
	}
	return graders, config, grader
}

// approveCase writes acceptance as the Case's approved list and a current
// Reference check under grader in which the Reference meets every item: the
// state in which a Case may run and be graded. The Case manifest must exist.
func approveCase(t *testing.T, caseDir, acceptance string, grader armFile) {
	t.Helper()
	writeFile(t, filepath.Join(caseDir, acceptanceName), acceptance)
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	scores := &gradeScores{Quality: 4, Rationale: "Reference.", ListRatio: 1}
	for i := range acceptanceBehaviours(acceptance) {
		scores.Items = append(scores.Items, itemGrade{Item: i + 1, Met: true, Reason: "Reference meets it."})
	}
	check := referenceCheck{AcceptanceDigest: acceptanceDigest(acceptance), Grader: grader.agentSpec(), ReferenceRange: manifest.ReferenceRange,
		Grade: &gradeRecord{Status: "graded", Scores: scores}, Accepted: []acceptedFailure{}}
	data, _ := json.Marshal(check)
	if err := os.MkdirAll(filepath.Join(caseDir, referenceCheckDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(caseDir, referenceCheckDirName, referenceCheckRecordName), string(data))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}
