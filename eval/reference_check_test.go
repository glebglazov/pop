package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceCheckGradesTheReferenceAndKeepsAcceptedReasons(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "parent\n")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "Parent standard.\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "feature.txt"), "reference\n")
	runGit(t, repo, "commit", "-qam", "reference")
	reference := parent + ".." + runGit(t, repo, "rev-parse", "HEAD")

	root := t.TempDir()
	cases, arms := filepath.Join(root, "cases"), filepath.Join(root, "arms")
	caseDir := filepath.Join(cases, "example")
	for _, dir := range []string{caseDir, arms} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(arms, "bare.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'opus'\n")
	graders, config, grader := testGrader(t, root)
	manifest, _ := json.Marshal(caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: reference, Scope: []string{"."}, GateCommands: []string{"cat feature.txt"}})
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(manifest))
	list := "# Acceptance list\n\nStatus: approved\n\n1. The file changes.\n2. The change is announced.\n"
	writeFile(t, filepath.Join(caseDir, acceptanceName), list)

	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "claude"), `#!/bin/sh
for argument in "$@"; do prompt="$argument"; done
case "$prompt" in
 "Read the file "*) prompt_file=${prompt#Read the file }; prompt_file=${prompt_file%% in full:*}; cp "$prompt_file" "$FAKE_GRADE_PROMPT" ;;
 *) printf '%s' "$prompt" > "$FAKE_GRADE_PROMPT" ;;
esac
[ "$(cat feature.txt)" = parent ] || exit 8
printf '%s\n' '{"type":"system","subtype":"init","model":"grader-test"}'
cat "$FAKE_GRADE_REPLY"
`)
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	promptPath, replyPath := filepath.Join(root, "prompt"), filepath.Join(root, "reply")
	t.Setenv("FAKE_GRADE_PROMPT", promptPath)
	t.Setenv("FAKE_GRADE_REPLY", replyPath)
	check := func(reply string) (referenceCheck, string) {
		t.Helper()
		data, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": reply, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
		writeFile(t, replyPath, string(data)+"\n")
		var progress bytes.Buffer
		args := []string{"--cases", cases, "--arms", arms, "--graders", graders, "--config", config, "--work", filepath.Join(root, "work"), "example"}
		if err := runReferenceCheckCommandWithProgress(args, evalProgress{out: &progress}); err != nil {
			t.Fatal(err)
		}
		var got referenceCheck
		decodeJSONFile(t, filepath.Join(caseDir, referenceCheckDirName, referenceCheckRecordName), &got)
		return got, progress.String()
	}

	got, progress := check("ITEM 1 MET: The file changes.\nITEM 2 NOT_MET: Nothing announces it.\nQUALITY 3: Adequate.")
	g := got.Grade
	if got.AcceptanceDigest != acceptanceDigest(list) || got.Grader != grader.agentSpec() || got.ReferenceRange != reference || len(got.Accepted) != 0 {
		t.Fatalf("check identity = %+v", got)
	}
	// The Reference diff is applied to the parent tree and gated as a Trial's
	// diff would be; the Grader reads it as the diff under grade.
	if g == nil || g.Status != "graded" || g.BaselineGates[0].Output != "parent\n" || g.Gates[0].Output != "reference\n" || !g.Scores.Items[0].Met || g.Scores.Items[1].Met {
		t.Fatalf("grade = %+v", g)
	}
	if prompt := readFile(t, promptPath); !strings.Contains(prompt, "+reference") || !strings.Contains(prompt, "2. The change is announced.") {
		t.Fatalf("Grader prompt:\n%s", prompt)
	}
	if !strings.Contains(progress, "Reference check Case=example Baseline gate finished: passed") || !strings.Contains(progress, "Reference check Case=example finished: grade=graded acceptance=1/2 quality=3/5 unaccepted=[2]") {
		t.Fatalf("progress:\n%s", progress)
	}
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "items [2]") {
		t.Fatalf("unaccepted failure error = %v", err)
	}

	got.Accepted = []acceptedFailure{{Item: 2, Behaviour: "The change is announced.", Reason: "The spec asks for it; the Reference missed it."}}
	data, _ := json.Marshal(got)
	writeFile(t, filepath.Join(caseDir, referenceCheckDirName, referenceCheckRecordName), string(data))
	if _, err := loadApprovedAcceptance(caseDir, grader); err != nil {
		t.Fatalf("accepted failure refused: %v", err)
	}

	// A new item moves the accepted behaviour to number 3: its reason follows
	// the behaviour, and the list is usable again without a second edit.
	list = "# Acceptance list\n\nStatus: approved\n\n1. The file changes.\n2. The file ends in a newline.\n3. The change is announced.\n"
	writeFile(t, filepath.Join(caseDir, acceptanceName), list)
	if _, err := loadApprovedAcceptance(caseDir, grader); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("list edit error = %v", err)
	}
	got, _ = check("ITEM 1 MET: The file changes.\nITEM 2 MET: It does.\nITEM 3 NOT_MET: Nothing announces it.\nQUALITY 3: Adequate.")
	if len(got.Accepted) != 1 || got.Accepted[0].Item != 3 || got.Accepted[0].Behaviour != "The change is announced." {
		t.Fatalf("carried reasons = %+v", got.Accepted)
	}
	if _, err := loadApprovedAcceptance(caseDir, grader); err != nil {
		t.Fatalf("carried reason refused: %v", err)
	}
}
