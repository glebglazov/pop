package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTicketPromptExactShape(t *testing.T) {
	ticket := "why is it broken? let's fix it\n"
	got := ticketPrompt(caseManifest{RepositoryURL: "https://example.test/repo.git", GateCommands: []string{"go build ./...", "go test ./..."}}, ticket)
	want := "Implement the request below in repository https://example.test/repo.git.\n" +
		"The project owner wrote it before any planning. Where it asks a question or asks for your opinion, decide the answer yourself and implement it: no one can answer you during this run.\n" +
		"Complete all requested work and run the gate commands before you finish.\nDo not make git commits.\n\nGate commands:\ngo build ./...\ngo test ./...\n\n## Request\n\n" + ticket
	if got != want {
		t.Fatalf("prompt = %q; want %q", got, want)
	}
}

func TestArmInput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bare.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'opus'\n")
	writeFile(t, filepath.Join(root, "ticket.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'opus'\ninput = 'ticket'\n")
	writeFile(t, filepath.Join(root, "pop-ticket.toml"), "kind = 'pop'\nagent = 'claude'\nmodel = 'opus'\ninput = 'ticket'\n")
	writeFile(t, filepath.Join(root, "other.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'opus'\ninput = 'summary'\n")
	if arm, err := loadArm(root, "bare"); err != nil || arm.Input != armInputSpec {
		t.Fatalf("default input: %+v, %v", arm, err)
	}
	if arm, err := loadArm(root, "ticket"); err != nil || arm.Input != armInputTicket {
		t.Fatalf("ticket input: %+v, %v", arm, err)
	}
	if arm, err := loadArm("arms", "ticket"); err != nil || arm.Kind != "bare" || arm.Input != armInputTicket {
		t.Fatalf("shipped Ticket arm: %+v, %v", arm, err)
	}
	if _, err := loadArm(root, "pop-ticket"); err == nil || !strings.Contains(err.Error(), "Pop arm cannot take the ticket") {
		t.Fatalf("Pop ticket error = %v", err)
	}
	if _, err := loadArm(root, "other"); err == nil || !strings.Contains(err.Error(), "spec or ticket") {
		t.Fatalf("unknown input error = %v", err)
	}
}

func TestTicketArmTrialGetsOnlyTheTicketBody(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "parent\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	root := t.TempDir()
	cases, arms, work, results := filepath.Join(root, "cases"), filepath.Join(root, "arms"), filepath.Join(root, "work"), filepath.Join(root, "results")
	caseDir := filepath.Join(cases, "example")
	for _, dir := range []string{caseDir, arms} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + "..HEAD", GateCommands: []string{"test ! -e new.txt"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(data))
	writeFile(t, filepath.Join(caseDir, "spec.md"), "The planned spec.\n")
	writeFile(t, filepath.Join(arms, "ticket.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'claude-test'\ninput = 'ticket'\n")
	graders, config, grader := testGrader(t, root)
	approveCase(t, caseDir, "Status: approved\n\n1. The file changes.\n", grader)
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "claude"), `#!/bin/sh
for argument in "$@"; do prompt="$argument"; done
case "$prompt" in
 "Read the file "*) prompt_file=${prompt#Read the file }; prompt_file=${prompt_file%% in full:*}; cp "$prompt_file" "$FAKE_TRIAL_PROMPT" ;;
 *) printf '%s' "$prompt" > "$FAKE_TRIAL_PROMPT" ;;
esac
printf 'new\n' > new.txt
printf '%s\n' '{"type":"system","subtype":"init","model":"claude-test"}'
printf '%s\n' '{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":1,"output_tokens":1}}'
`)
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	promptPath := filepath.Join(root, "prompt.txt")
	t.Setenv("FAKE_TRIAL_PROMPT", promptPath)
	args := []string{"run", "--case", "example", "--arm", "ticket", "--cases", cases, "--arms", arms, "--graders", graders, "--config", config, "--work", work, "--results", results}

	ticket := "# Ticket\n\nStatus: draft\nSource: session 1234\n\n---\n\nwhy does the file not change? let's fix it\n"
	writeFile(t, filepath.Join(caseDir, ticketName), ticket)
	if err := run(args); err == nil || !strings.Contains(err.Error(), `Arm "ticket" needs an approved ticket for Case example`) {
		t.Fatalf("draft ticket error = %v", err)
	}
	if _, err := os.Stat(results); !os.IsNotExist(err) {
		t.Fatalf("a draft ticket created results: %v", err)
	}

	writeFile(t, filepath.Join(caseDir, ticketName), strings.Replace(ticket, "Status: draft", "Status: approved", 1))
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, promptPath), ticketPrompt(manifest, "why does the file not change? let's fix it\n"); got != want {
		t.Fatalf("captured prompt = %q; want %q", got, want)
	}
	var record trialRecord
	decodeJSONFile(t, filepath.Join(results, "example", "ticket", "01", "trial.json"), &record)
	if record.Outcome != outcomeCompleted || record.Grade == nil || record.Grade.Status != "gate_failed" {
		t.Fatalf("record = %+v", record)
	}
}

func TestRollupScoresEachTrialWithoutItsNameOnlyItems(t *testing.T) {
	root, cases := t.TempDir(), t.TempDir()
	list := "Status: approved\nName-only items: 2, 3\n\n1. Work is done.\n2. The status is EXPLORE-FAILED.\n3. The key is `explorer`.\n4. Work is kept.\n"
	if err := os.MkdirAll(filepath.Join(cases, "case-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cases, "case-a", acceptanceName), list)
	items := []itemGrade{{Item: 1, Met: true}, {Item: 2, Met: false}, {Item: 3, Met: false}, {Item: 4, Met: false}}
	record := trialRecord{Case: "case-a", Arm: "ticket", Repeat: 1, Outcome: outcomeCompleted,
		Grade: &gradeRecord{Status: "graded", AcceptanceDigest: acceptanceDigest(list), Scores: &gradeScores{Items: items, ListRatio: 0.25, Quality: 3}}}
	dir := filepath.Join(root, "case-a", "ticket", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(record)
	writeFile(t, filepath.Join(dir, "trial.json"), string(data))
	rollup, err := loadEvalRollup(root, cases)
	if err != nil {
		t.Fatal(err)
	}
	row := rollup.Rows[0]
	if row.AcceptanceRatio == nil || *row.AcceptanceRatio != 0.25 || row.NameFreeAcceptanceRatio == nil || *row.NameFreeAcceptanceRatio != 0.5 || row.Stale != 0 {
		t.Fatalf("row = %+v", row)
	}

	writeFile(t, filepath.Join(cases, "case-a", acceptanceName), strings.Replace(list, "2, 3", "2, 5", 1))
	if _, err := loadEvalRollup(root, cases); err == nil || !strings.Contains(err.Error(), `name-only item "5" is not an item number from 1 to 4`) {
		t.Fatalf("out-of-range mark error = %v", err)
	}
}
