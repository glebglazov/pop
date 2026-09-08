package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBarePromptExactShape(t *testing.T) {
	spec := "  # Keep spacing\r\n\nImplement this.  "
	got := barePrompt(caseManifest{RepositoryURL: "https://example.test/repo.git", GateCommands: []string{"go build ./...", "go test ./..."}}, spec)
	want := "Implement the spec below in repository https://example.test/repo.git.\nComplete all requested work and run the gate commands before you finish.\nDo not make git commits.\n\nGate commands:\ngo build ./...\ngo test ./...\n\n## Spec\n\n" + spec
	if got != want {
		t.Fatalf("prompt = %q; want %q", got, want)
	}
}

func TestBareTrialCommand(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "parent\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "feature.txt"), "later\n")
	runGit(t, repo, "commit", "-qam", "later")
	root := t.TempDir()
	cases, arms, work, results := filepath.Join(root, "cases"), filepath.Join(root, "arms"), filepath.Join(root, "work"), filepath.Join(root, "results")
	caseDir := filepath.Join(cases, "example")
	for _, dir := range []string{caseDir, arms} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + "..HEAD", GateCommands: []string{"false"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(data))
	spec := "Change the file.\n"
	writeFile(t, filepath.Join(caseDir, "spec.md"), spec)
	writeFile(t, filepath.Join(caseDir, acceptanceName), "Status: draft\n")
	writeFile(t, filepath.Join(arms, "anonymous-arm.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'claude-test'\n")
	args := []string{"run", "--case", "example", "--arm", "anonymous-arm", "--cases", cases, "--arms", arms, "--work", work, "--results", results}
	if err := run(args); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("approval error = %v", err)
	}
	for _, dir := range []string{work, results} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("unapproved Trial created %s", dir)
		}
	}
	writeFile(t, filepath.Join(caseDir, acceptanceName), "Status: approved\n\n1. The file changes.\n")
	bin := t.TempDir()
	agent := `#!/bin/sh
for argument in "$@"; do prompt="$argument"; done
case "$prompt" in
 "Read the file "*) prompt_file=${prompt#Read the file }; prompt_file=${prompt_file%% in full:*}; cp "$prompt_file" "$FAKE_TRIAL_PROMPT" ;;
 *) printf '%s' "$prompt" > "$FAKE_TRIAL_PROMPT" ;;
esac
[ "$(cat feature.txt)" = parent ] || exit 8
git symbolic-ref -q HEAD && exit 9
printf 'changed\n' > feature.txt
printf 'new\n' > new.txt
printf '\000\001\002' > binary.dat
printf '%s\n' '{"type":"system","subtype":"init","model":"claude-test"}'
case "$FAKE_TRIAL_MODE" in
 timeout) sleep 30 ;;
 crash) exit 7 ;;
 quota) cat "$FAKE_TRIAL_QUOTA"; exit 1 ;;
 *) printf '%s\n' '{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":123,"output_tokens":45}}' ;;
esac
`
	writeFile(t, filepath.Join(bin, "claude"), agent)
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	promptPath := filepath.Join(root, "prompt.txt")
	t.Setenv("FAKE_TRIAL_PROMPT", promptPath)
	fixture, err := os.Open("../tasks/testdata/streams/claude-session-limit.events.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	reader, err := gzip.NewReader(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	var quota strings.Builder
	for {
		var event struct {
			Raw string `json:"raw"`
		}
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		quota.WriteString(event.Raw + "\n")
	}
	quotaPath := filepath.Join(root, "quota.jsonl")
	writeFile(t, quotaPath, quota.String())
	t.Setenv("FAKE_TRIAL_QUOTA", quotaPath)

	var previousWork string
	for i, tc := range []struct{ mode, outcome, agentOutcome string }{
		{"success", "completed", "completed"}, {"timeout", "timed_out", "timed_out"}, {"quota", "invalid", "quota_paused"}, {"crash", "invalid", "failed"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("FAKE_TRIAL_MODE", tc.mode)
			repeat := string(rune('1' + i))
			ceiling := "1m"
			if tc.mode == "timeout" {
				ceiling = "500ms"
			}
			trialArgs := append(append([]string{}, args...), "--repeat", repeat, "--ceiling", ceiling)
			var progress bytes.Buffer
			waits, stopped := []evalWait{}, 0
			if err := runTrialCommandWithProgress(trialArgs[1:], evalProgress{out: &progress, waiter: recordingWaiter(&waits, &stopped)}); err != nil {
				t.Fatal(err)
			}
			if stopped != len(waits) {
				t.Fatalf("waiting reporters: started=%d stopped=%d", len(waits), stopped)
			}
			if tc.mode == "success" {
				if got := strings.Join(waitPhases(waits), ","); got != "repository preparation,Bare-agent invocation,grading repository preparation,Objective gate 1/1" {
					t.Fatalf("waiting phases = %s", got)
				}
				if waits[1].ceiling != time.Minute || waits[0].ceiling != 0 || waits[2].ceiling != 0 || waits[3].ceiling != 0 {
					t.Fatalf("waiting ceilings = %+v", waits)
				}
				assertProgressOrder(t, progress.String(),
					"Eval Matrix: Trials=1 results=",
					"Trial 1/1 Case=example Arm=anonymous-arm repeat=1 attempt=1",
					"preparation started", "preparation finished",
					"Arm execution started", "Arm execution finished: outcome=completed",
					"patch saving started", "patch saving finished:",
					"Objective gate started: false", "Objective gate finished: failed exit=1 command=false",
					"Grader execution skipped: one or more Objective gates failed",
					"finished: outcome=completed grade=gate_failed acceptance=0 quality=0/5 result=",
					"Eval Matrix finished:", "Read the Rollup:")
			}
			dir := filepath.Join(results, "example", "anonymous-arm", "0"+repeat)
			var record trialRecord
			decodeJSONFile(t, filepath.Join(dir, "trial.json"), &record)
			if record.Outcome != tc.outcome || record.AgentOutcome != tc.agentOutcome || record.Case != "example" || record.Arm != "anonymous-arm" || record.Repeat != i+1 || record.Model != "claude-test" || record.RunID == "" || !record.EndedAt.After(record.StartedAt) {
				t.Fatalf("record = %+v", record)
			}
			if record.WorkDir == previousWork || strings.Contains(record.WorkDir, "anonymous-arm") {
				t.Fatalf("clone is not fresh and neutral: %s", record.WorkDir)
			}
			previousWork = record.WorkDir
			clone := filepath.Join(record.WorkDir, "repository")
			if head := runGit(t, clone, "rev-parse", "HEAD"); head != parent {
				t.Fatalf("HEAD = %s", head)
			}
			if got := readFile(t, promptPath); got != barePrompt(manifest, spec) {
				t.Fatalf("captured prompt = %q", got)
			}
			patch := readFile(t, filepath.Join(dir, "diff.patch"))
			for _, text := range []string{"-parent", "+changed", "+new", "GIT binary patch"} {
				if !strings.Contains(patch, text) {
					t.Fatalf("patch missing %q: %s", text, patch)
				}
			}
			if strings.Contains(patch, "anonymous-arm") || strings.Contains(patch, record.WorkDir) {
				t.Fatalf("patch leaks Trial identity: %s", patch)
			}
			if tc.mode == "success" && (!record.Spend.Tokens.HasInput || record.Spend.Tokens.Input != 123) {
				t.Fatalf("spend = %+v", record.Spend)
			}
			captures, err := os.ReadDir(filepath.Join(record.WorkDir, "capture"))
			if err != nil || len(captures) != 2 {
				t.Fatalf("capture = %v, %v", captures, err)
			}
			progress.Reset()
			if err := runTrialCommandWithProgress(trialArgs[1:], evalProgress{out: &progress}); err != nil {
				t.Fatalf("resume existing repeat: %v", err)
			}
			if tc.mode == "success" && !strings.Contains(progress.String(), "skipped: saved Trial already has grade=gate_failed") {
				t.Fatalf("resume progress:\n%s", progress.String())
			}
		})
	}
	if got := readFile(t, filepath.Join(repo, "feature.txt")); got != "later\n" {
		t.Fatalf("source changed: %q", got)
	}
}

func assertProgressOrder(t *testing.T, output string, messages ...string) {
	t.Helper()
	position := 0
	for _, message := range messages {
		next := strings.Index(output[position:], message)
		if next < 0 {
			t.Fatalf("progress missing %q after byte %d:\n%s", message, position, output)
		}
		position += next + len(message)
	}
}

func TestArmFiles(t *testing.T) {
	if arm, err := loadArm("arms", "bare"); err != nil || arm.Kind != "bare" {
		t.Fatalf("shipped Arm: %+v, %v", arm, err)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pop.toml"), "kind = 'pop'\nagent = 'claude'\nmodel = 'opus'\n[config.tasks]\nmax_tries = 3\n[manifest]\nverify = false\n")
	arm, err := loadArm(root, "pop")
	if err != nil || arm.Manifest["verify"] != false || arm.Config["tasks"] == nil {
		t.Fatalf("Pop overrides: %+v, %v", arm, err)
	}
}
