package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
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
	writeFile(t, filepath.Join(graders, "grader.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'claude-test'\nreasoning = 'high'\n")
	config := filepath.Join(root, "config.toml")
	writeFile(t, config, "grader_arm = 'grader'\n")
	acceptance := "# Acceptance list\n\nStatus: approved\n\n1. The file changes.\n2. Compatibility is retained.\n"
	grader, err := loadGrader(config, graders)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	agent := `#!/bin/sh
[ -f "$CLAUDE_CONFIG_DIR/settings.json" ] || exit 6
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
sample=$(($(cat "$FAKE_GRADE_COUNT" 2>/dev/null || echo 0) + 1))
echo "$sample" > "$FAKE_GRADE_COUNT"
if [ -f "$FAKE_GRADE_REPLY.$sample" ]; then cat "$FAKE_GRADE_REPLY.$sample"; else cat "$FAKE_GRADE_REPLY"; fi
`
	writeFile(t, filepath.Join(bin, "claude"), agent)
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Objective gate output is parsed. The gate must keep its bare shell even
	// when the human's configured shell is unusable.
	t.Setenv("SHELL", filepath.Join(root, "must-not-run-human-shell"))
	for _, tc := range []struct {
		name, reply, gate, status string
		// later are the replies of samples 2 and 3; without them every sample
		// gives reply.
		later []string
	}{
		{name: "pass", reply: "ITEM 1 MET: The file changes.\nITEM 2 NOT_MET: Compatibility was removed.\nQUALITY 4: Clear implementation.", gate: "true", status: "graded"},
		// The samples disagree on item 2 and on quality: the grade is their
		// majority and median, not the first sample's reply.
		{name: "split", reply: "ITEM 1 MET: The file changes.\nITEM 2 MET: Compatibility looks kept.\nQUALITY 2: Weak tests.", gate: "true", status: "graded", later: []string{
			"ITEM 1 MET: It changes.\nITEM 2 NOT_MET: Compatibility was removed.\nQUALITY 4: Clear implementation.",
			"ITEM 1 MET: It changes.\nITEM 2 NOT_MET: Another reason.\nQUALITY 5: Excellent.",
		}},
		{name: "fail", reply: "must not run", gate: "test \"$(cat feature.txt)\" = parent || { printf 'gate failed'; exit 7; }", status: "gate_failed"},
		// The parent already fails this gate, so its failure says nothing about
		// the Arm: the Trial must not be scored at all.
		{name: "baseline", reply: "must not run", gate: "test \"$(cat feature.txt)\" = changed || { printf 'parent fails'; exit 5; }", status: "baseline_failed"},
		{name: "unparseable", reply: "This is the whole unparseable reply.\nNo scores here.", gate: "true", status: "ungraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + "..HEAD", Scope: []string{"feature.txt"}, GateCommands: []string{
				"cat feature.txt; if [ -f binary.dat ] && [ -f outside.txt ]; then echo restored; fi", tc.gate, "printf 'last gate ran' > gate-output.txt; cat gate-output.txt",
			}}
			data, _ := json.Marshal(manifest)
			writeFile(t, filepath.Join(caseDir, caseManifestName), string(data))
			approveCase(t, caseDir, acceptance, grader.armFile)
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
			for i, reply := range append([]string{tc.reply}, tc.later...) {
				path := replyPath
				if i > 0 {
					path = fmt.Sprintf("%s.%d", replyPath, i+1)
				}
				data, _ = json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": reply, "usage": map[string]int{"input_tokens": 123, "output_tokens": 45}})
				writeFile(t, path, string(data)+"\n")
			}
			t.Setenv("FAKE_GRADE_COUNT", filepath.Join(root, tc.name+"-count"))
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
				want := "grading repository preparation,Baseline gate 1/3,Baseline gate 2/3,Baseline gate 3/3,Objective gate 1/3,Objective gate 2/3,Objective gate 3/3,Grader repository preparation,Grader invocation 1/3,Grader invocation 2/3,Grader invocation 3/3"
				if got := strings.Join(waitPhases(waits), ","); got != want || waits[len(waits)-1].ceiling != time.Hour {
					t.Fatalf("waiting phases = %s, waits = %+v", got, waits)
				}
			}
			var got trialRecord
			decodeJSONFile(t, filepath.Join(resultDir, "trial.json"), &got)
			g := got.Grade
			if g == nil || g.Status != tc.status || g.AcceptanceDigest != acceptanceDigest(acceptance) || len(g.BaselineGates) != 3 || g.BaselineGates[0].Output != "parent\n" || g.BaselineGates[2].Output != "last gate ran" {
				t.Fatalf("grade = %+v", g)
			}
			if got.Spend.Tokens.Input != 999 {
				t.Fatalf("Arm spend changed: %+v", got.Spend)
			}
			if tc.status == "baseline_failed" {
				assertProgressOrder(t, progress.String(), "Baseline gate finished: failed exit=5", "Objective gates and Grader execution skipped:", "finished: outcome=completed grade=baseline_failed acceptance=n/a quality=n/a")
				if g.BaselineGates[1].ExitCode != 5 || len(g.Gates) != 0 || g.Scores != nil || len(g.Samples) != 0 {
					t.Fatalf("baseline failure = %+v", g)
				}
				if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
					t.Fatal("Grader ran after baseline failure")
				}
				return
			}
			if len(g.Gates) != 3 || g.Gates[0].Output != "changed\nrestored\n" || g.Gates[2].Output != "last gate ran" {
				t.Fatalf("Trial-tree gates = %+v", g.Gates)
			}
			if tc.status == "graded" && g.AgentHomeDigest != agentHomeDigest() {
				t.Fatalf("grade agent home = %q, want the checked-in home", g.AgentHomeDigest)
			}
			if tc.status == "gate_failed" {
				assertProgressOrder(t, progress.String(), "grading preparation started", "grading preparation finished", "Objective gate started:", "Objective gate finished:", "Grader execution skipped:", "finished: outcome=completed grade=gate_failed acceptance=0 quality=0/5")
				if g.Gates[1].ExitCode != 7 || g.Scores == nil || g.Scores.Quality != 0 || g.Scores.ListRatio != 0 || len(g.Samples) != 0 {
					t.Fatalf("gate failure = %+v", g)
				}
				if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
					t.Fatal("Grader ran after gate failure")
				}
				return
			}
			// A failed sample stops the grade: the later samples would only spend.
			wantSamples := 3
			if tc.status == "ungraded" {
				wantSamples = 1
			}
			if len(g.Samples) != wantSamples {
				t.Fatalf("samples = %+v", g.Samples)
			}
			runs := map[string]bool{}
			for _, sample := range g.Samples {
				if sample.Grader == nil || sample.Grader.Spend.Tokens.Input != 123 || !sample.Grader.Spend.Tokens.HasInput || sample.Grader.RunID == "" || runs[sample.Grader.RunID] {
					t.Fatalf("Grader spend = %+v", sample.Grader)
				}
				runs[sample.Grader.RunID] = true
			}
			if g.Samples[0].Reply != tc.reply+"\n" {
				t.Fatalf("reply = %q", g.Samples[0].Reply)
			}
			if tc.status == "ungraded" {
				if !strings.Contains(progress.String(), "Grader sample 1/3 finished: outcome=completed grade=ungraded reason=") {
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
			if tc.status == "graded" {
				wantSplit := tc.name == "split"
				if g.Scores.Items[0].Split || g.Scores.Items[1].Split != wantSplit {
					t.Fatalf("split items = %+v", g.Scores.Items)
				}
				want := "grade=graded split=0"
				if wantSplit {
					want = "grade=graded split=1"
				}
				if !strings.Contains(progress.String(), want) {
					t.Fatalf("split progress:\n%s", progress.String())
				}
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

func TestLostTrialIsExcludedFromGrading(t *testing.T) {
	record := trialRecord{Case: "case", Arm: "bare", Repeat: 1, Outcome: outcomeLost, Grade: &gradeRecord{Status: "ungraded"}}
	var progress bytes.Buffer
	err := gradeOneTrial(caseManifest{}, "", 0, &record, gradeOptions{progress: evalProgress{out: &progress}})
	if err != nil || record.Grade.Reason != "Lost Trial is excluded" || !strings.Contains(progress.String(), "grading skipped: Lost Trial is excluded") {
		t.Fatalf("Lost Trial grade = %+v, progress=%q, error=%v", record.Grade, progress.String(), err)
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

func TestLoadGraderPinsReasoningAndSamples(t *testing.T) {
	grader, err := loadGrader("config.toml", "graders")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tasks.ResolveAgentInvocation(grader.agentSpec(), "", "", ".")
	if err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(invocation.Args, " "); !strings.Contains(args, `-c model_reasoning_effort="high"`) || grader.samples != 3 {
		t.Fatalf("checked-in Grader args = %s, samples = %d", args, grader.samples)
	}

	root := t.TempDir()
	graders := filepath.Join(root, "graders")
	if err := os.MkdirAll(graders, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, arm, config, refusal string }{
		{"no reasoning", "kind = 'bare'\nagent = 'codex'\nmodel = 'gpt-test'\n", "grader_arm = 'grader'\n", "Grader must set reasoning"},
		{"even samples", "kind = 'bare'\nagent = 'codex'\nmodel = 'gpt-test'\nreasoning = 'high'\n", "grader_arm = 'grader'\ngrader_samples = 2\n", "odd number"},
		{"no samples", "kind = 'bare'\nagent = 'codex'\nmodel = 'gpt-test'\nreasoning = 'high'\n", "grader_arm = 'grader'\ngrader_samples = 0\n", "odd number"},
		{"preset without effort", "kind = 'bare'\nagent = 'opencode'\nmodel = 'gpt-test'\nreasoning = 'high'\n", "grader_arm = 'grader'\n", "cannot set a reasoning effort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, filepath.Join(graders, "grader.toml"), tc.arm)
			config := filepath.Join(root, "config.toml")
			writeFile(t, config, tc.config)
			if _, err := loadGrader(config, graders); err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("error = %v, want %q", err, tc.refusal)
			}
		})
	}
}
