package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGradeTrialCommand(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "parent\n")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "Parent standard: keep public behaviour stable.\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "feature.txt"), "changed\n")
	writeFile(t, filepath.Join(repo, "outside.txt"), "new\n")
	writeFile(t, filepath.Join(repo, "binary.dat"), "\x00\x01\x02")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "Trial standard must not become the yardstick.\n")
	patch, err := trialPatch(repo, parent)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "reset", "--hard", parent)
	runGit(t, repo, "clean", "-fd")
	writeFile(t, filepath.Join(repo, "feature.txt"), "SECRET_REFERENCE_IMPLEMENTATION\n")
	runGit(t, repo, "commit", "-qam", "later")
	root := t.TempDir()
	caseDir, arms, graders := filepath.Join(root, "cases", "example"), filepath.Join(root, "arms"), filepath.Join(root, "graders")
	for _, dir := range []string{caseDir, arms, graders} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(arms, "hidden-arm-name.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'opus'\n")
	writeFile(t, filepath.Join(graders, "grader.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'claude-test'\n")
	config := filepath.Join(root, "config.toml")
	writeFile(t, config, "grader_arm = 'grader'\n")
	writeFile(t, filepath.Join(caseDir, acceptanceName), "# Acceptance list\n\nStatus: approved\n\n1. The file changes.\n2. Compatibility is retained.\n")
	bin := t.TempDir()
	agent := `#!/bin/sh
for argument in "$@"; do prompt="$argument"; done
case "$prompt" in
 "Read the file "*) prompt_file=${prompt#Read the file }; prompt_file=${prompt_file%% in full:*}; cp "$prompt_file" "$FAKE_GRADE_PROMPT" ;;
 *) printf '%s' "$prompt" > "$FAKE_GRADE_PROMPT" ;;
esac
[ "$(cat feature.txt)" = parent ] || exit 8
[ ! -e outside.txt ] || exit 9
[ ! -e gate-output.txt ] || exit 10
git symbolic-ref -q HEAD && exit 11
printf '%s\n' '{"type":"system","subtype":"init","model":"claude-test"}'
cat "$FAKE_GRADE_REPLY"
`
	writeFile(t, filepath.Join(bin, "claude"), agent)
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct{ name, reply, gate, status string }{
		{"pass", "ITEM 1 MET: The file changes.\nITEM 2 NOT_MET: Compatibility was removed.\nQUALITY 4: Clear implementation.", "true", "graded"},
		{"fail", "must not run", "printf 'gate failed'; exit 7", "gate_failed"},
		{"unparseable", "This is the whole unparseable reply.\nNo scores here.", "true", "ungraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + "..HEAD", Scope: []string{"feature.txt"}, GateCommands: []string{
				"test \"$(cat feature.txt)\" = changed && test -f binary.dat && test -f outside.txt", tc.gate, "printf 'last gate ran' > gate-output.txt; cat gate-output.txt",
			}}
			data, _ := json.Marshal(manifest)
			writeFile(t, filepath.Join(caseDir, caseManifestName), string(data))
			results := filepath.Join(root, tc.name)
			resultDir := filepath.Join(results, "example", "hidden-arm-name", "01")
			if err := os.MkdirAll(resultDir, 0o755); err != nil {
				t.Fatal(err)
			}
			record := trialRecord{Case: "example", Arm: "hidden-arm-name", Repeat: 1, Model: "opus", Outcome: "completed"}
			record.Spend.Tokens.Input, record.Spend.Tokens.HasInput = 999, true
			data, _ = json.Marshal(record)
			writeFile(t, filepath.Join(resultDir, "trial.json"), string(data))
			writeFile(t, filepath.Join(resultDir, "diff.patch"), string(patch))
			promptPath := filepath.Join(root, tc.name+"-prompt")
			replyPath := filepath.Join(root, tc.name+"-reply")
			data, _ = json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": tc.reply, "usage": map[string]int{"input_tokens": 123, "output_tokens": 45}})
			writeFile(t, replyPath, string(data)+"\n")
			t.Setenv("FAKE_GRADE_PROMPT", promptPath)
			t.Setenv("FAKE_GRADE_REPLY", replyPath)
			args := []string{"grade", "--cases", filepath.Dir(caseDir), "--arms", arms, "--graders", graders, "--config", config, "--work", filepath.Join(root, "work"), "--results", results, "example", "hidden-arm-name", "1"}
			var progress bytes.Buffer
			waits, stopped := []evalWait{}, 0
			if err := runGradeCommandWithProgress(args[1:], evalProgress{out: &progress, waiter: recordingWaiter(&waits, &stopped)}, true); err != nil {
				t.Fatal(err)
			}
			if stopped != len(waits) {
				t.Fatalf("waiting reporters: started=%d stopped=%d", len(waits), stopped)
			}
			if tc.status == "graded" {
				want := "grading repository preparation,Objective gate 1/3,Objective gate 2/3,Objective gate 3/3,Grader repository preparation,Grader invocation"
				if got := strings.Join(waitPhases(waits), ","); got != want || waits[len(waits)-1].ceiling != time.Hour {
					t.Fatalf("waiting phases = %s, waits = %+v", got, waits)
				}
			}
			var got trialRecord
			decodeJSONFile(t, filepath.Join(resultDir, "trial.json"), &got)
			g := got.Grade
			if g == nil || g.Status != tc.status || len(g.Gates) != 3 || g.Gates[0].ExitCode != 0 || g.Gates[2].Output != "last gate ran" {
				t.Fatalf("grade = %+v", g)
			}
			if got.Spend.Tokens.Input != 999 {
				t.Fatalf("Arm spend changed: %+v", got.Spend)
			}
			if tc.status == "gate_failed" {
				assertProgressOrder(t, progress.String(), "grading preparation started", "grading preparation finished", "Objective gate started:", "Objective gate finished:", "Grader execution skipped:", "finished: outcome=completed grade=gate_failed acceptance=0 quality=0/5")
				if g.Gates[1].ExitCode != 7 || g.Scores == nil || g.Scores.Quality != 0 || g.Scores.ListRatio != 0 || g.Grader != nil {
					t.Fatalf("gate failure = %+v", g)
				}
				if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
					t.Fatal("Grader ran after gate failure")
				}
				return
			}
			if g.Grader == nil || g.Grader.Spend.Tokens.Input != 123 || !g.Grader.Spend.Tokens.HasInput || g.Grader.RunID == "" {
				t.Fatalf("Grader spend = %+v", g.Grader)
			}
			if g.Reply != tc.reply+"\n" {
				t.Fatalf("reply = %q", g.Reply)
			}
			if tc.status == "ungraded" {
				if !strings.Contains(progress.String(), "Grader execution finished: outcome=completed grade=ungraded reason=") {
					t.Fatalf("malformed Grader progress:\n%s", progress.String())
				}
				if g.Scores != nil || g.Reason == "" {
					t.Fatalf("unparseable grade = %+v", g)
				}
			} else if g.Scores == nil || g.Scores.Quality != 4 || g.Scores.ListRatio != 0.5 || len(g.Scores.Items) != 2 || !g.Scores.Items[0].Met || g.Scores.Items[1].Met || g.Scores.Items[1].Reason != "Compatibility was removed." || g.Scores.Rationale != "Clear implementation." {
				t.Fatalf("scores = %+v", g.Scores)
			}
			if tc.status == "graded" && !strings.Contains(progress.String(), "finished: outcome=completed grade=graded acceptance=1/2 quality=4/5 result=") {
				t.Fatalf("graded completion progress:\n%s", progress.String())
			}
			prompt := readFile(t, promptPath)
			for _, text := range []string{"+changed", "1. The file changes.", "> Parent standard: keep public behaviour stable.", "Files outside stated scope: [\"AGENTS.md\" \"binary.dat\" \"outside.txt\"]", "correctness, maintainability, clarity, test adequacy, and unnecessary complexity"} {
				if !strings.Contains(prompt, text) {
					t.Fatalf("prompt missing %q", text)
				}
			}
			for _, text := range []string{"hidden-arm-name", "SECRET_REFERENCE_IMPLEMENTATION", "Reference diff", "opus", resultDir} {
				if strings.Contains(prompt, text) {
					t.Fatalf("prompt discloses %q", text)
				}
			}
		})
	}
}

func TestGraderReplyRejectsIncompleteOrAmbiguousScores(t *testing.T) {
	for _, reply := range []string{
		"ITEM 1 MET: yes\nQUALITY 0: bad", "ITEM 1 MET: yes\nQUALITY 6: great",
		"ITEM 2 MET: yes\nQUALITY 3: fine", "ITEM 1 MAYBE: yes\nQUALITY 3: fine",
		"ITEM 1 MET: \nQUALITY 3: fine", "ITEM 1 MET: yes\nQUALITY 3: ",
		"ITEM 1 MET: yes\nQUALITY 3: fine\nExtra prose", "QUALITY 3: fine",
	} {
		if scores, err := parseGraderReply(reply, 1); err == nil || scores != nil {
			t.Fatalf("accepted %q", reply)
		}
	}
}
