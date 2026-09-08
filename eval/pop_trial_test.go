package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
)

func TestPopTrialCommand(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "source")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	writeFile(t, filepath.Join(repo, "feature.txt"), "parent\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "parent")
	parent := runGit(t, repo, "rev-parse", "HEAD")
	cases := filepath.Join(root, "cases")
	caseDir := filepath.Join(cases, "example")
	if err := os.MkdirAll(filepath.Join(caseDir, "tasks"), 0755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(caseManifest{Name: "example", RepositoryURL: repo, ParentCommit: parent, ReferenceRange: parent + "..HEAD", GateCommands: []string{"false"}})
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(raw))
	writeFile(t, filepath.Join(caseDir, "spec.md"), "Change the file.\n")
	writeFile(t, filepath.Join(caseDir, acceptanceName), "Status: approved\n\n1. File changes.\n")
	writeFile(t, filepath.Join(caseDir, "tasks", "index.json"), `{"tasks":[{"id":"change","file":"change.md","title":"Change","type":"AFK","status":"open","blocked_by":[]}]}`)
	writeFile(t, filepath.Join(caseDir, "tasks", "change.md"), "## Acceptance criteria\n\n- [ ] File changes.\n")
	human := filepath.Join(root, "human-data")
	humanConfig := filepath.Join(root, "human-config")
	for _, dir := range []string{human, humanConfig} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "sentinel"), "untouched")
	}
	t.Setenv("XDG_DATA_HOME", human)
	t.Setenv("XDG_CONFIG_HOME", humanConfig)
	t.Setenv("TMUX", "human-server")
	t.Setenv("TMUX_PANE", "%99")
	log := filepath.Join(root, "commands")
	t.Setenv("EVAL_COMMAND_LOG", log)
	binary := filepath.Join(root, "pop")
	writeFile(t, binary, `#!/bin/sh
[ "$#" -ge 3 ] || exit 9
[ "$1" = tasks ] || exit 10
[ "$3" = example ] || exit 11
[ -z "$TMUX$TMUX_PANE" ] || exit 12
[ "$NO_COLOR" = 1 ] || exit 13
[ "$TERM" = dumb ] || exit 14
if read input; then exit 15; fi
printf '%s|%s|%s|%s\n' "$*" "$PWD" "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" >> "$EVAL_COMMAND_LOG"
case "$2" in
 register)
  [ "$#" = 3 ] || exit 16
  test -f "$XDG_CONFIG_HOME/pop/config.toml" || exit 17
  test -f "$XDG_DATA_HOME"/pop/repos/*/tasks/example/change.md || exit 18
  mkdir -p "$XDG_DATA_HOME/pop/capture-test"
  printf captured > "$XDG_DATA_HOME/pop/capture-test/run"
  ;;
 implement)
  printf 'changed\n' > feature.txt
  printf 'new\n' > new.txt
  case "$EVAL_POP_STATUS" in TIMEOUT) exec sleep 30 ;; FAILED|VERIFY-FAILED) exit 1 ;; CRASH) exit 7 ;; esac
  ;;
 status)
  status=$EVAL_POP_STATUS
  case "$status" in TIMEOUT|CRASH) status=READY ;; esac
  printf 'example  [%s]  1/1\n' "$status"
  ;;
 spend)
  [ "$4" = --json ] || exit 19
  printf '%s\n' '{"task_set_id":"example","implement_input_tokens":123,"verification_input_tokens":45,"refine_input_tokens":67,"rows":[{"turns":3,"peak_input_tokens":123}]}'
  ;;
 *) exit 20 ;;
esac
`)
	if err := os.Chmod(binary, 0755); err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"DONE", "FAILED", "VERIFY-FAILED", "TIMEOUT", "CRASH"} {
		t.Run(status, func(t *testing.T) {
			t.Setenv("EVAL_POP_STATUS", status)
			writeFile(t, log, "")
			ceiling := "1m"
			if status == "TIMEOUT" {
				ceiling = "300ms"
			}
			results := filepath.Join(root, "results")
			var progress bytes.Buffer
			waits, stopped := []evalWait{}, 0
			err := runTrialCommandWithProgress([]string{"--case", "example", "--arm", "pop", "--cases", cases, "--arms", "arms", "--work", filepath.Join(root, "work"), "--results", results, "--pop", binary, "--repeat", fmt.Sprint(i + 1), "--ceiling", ceiling}, evalProgress{out: &progress, waiter: recordingWaiter(&waits, &stopped)})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if stopped != len(waits) {
				t.Fatalf("waiting reporters: started=%d stopped=%d", len(waits), stopped)
			}
			if status == "DONE" {
				if !strings.Contains(strings.Join(waitPhases(waits), ","), "repository preparation,Pop drain,grading repository preparation,Objective gate 1/1") {
					t.Fatalf("waiting phases = %+v", waits)
				}
				assertProgressOrder(t, progress.String(), "Arm=pop repeat=1 attempt=1", "preparation started", "Arm execution started", "Arm execution finished: outcome=completed", "patch saving finished:", "Objective gate started: false", "Grader execution skipped:", "finished: outcome=completed grade=gate_failed")
			}
			if status == "TIMEOUT" && !strings.Contains(progress.String(), "grading skipped: Trial ceiling reached; zero scores recorded") {
				t.Fatalf("Trial ceiling progress:\n%s", progress.String())
			}
			if status == "CRASH" {
				assertProgressOrder(t, progress.String(), "Arm execution finished: outcome=invalid", "Invalid Trial retry attempt=2 reason=pop tasks implement example exited 7", "Arm execution started", "grading skipped: Invalid Trial is excluded", "finished: outcome=invalid grade=ungraded")
			}
			result := filepath.Join(results, "example", "pop", fmt.Sprintf("%02d", i+1))
			var record trialRecord
			decodeJSONFile(t, filepath.Join(result, "trial.json"), &record)
			wantOutcome, wantStatus := "completed", status
			if status == "TIMEOUT" {
				wantOutcome, wantStatus = "timed_out", "READY"
			}
			if status == "CRASH" {
				wantOutcome, wantStatus = "invalid", "READY"
			}
			if record.Outcome != wantOutcome || record.SetStatus != wantStatus {
				t.Fatalf("record: %+v", record)
			}
			var spend map[string]any
			if err := json.Unmarshal(record.SetSpend, &spend); err != nil {
				t.Fatal(err)
			}
			if spend["verification_input_tokens"] != float64(45) {
				t.Fatalf("spend: %s", record.SetSpend)
			}
			patch := readFile(t, filepath.Join(result, "diff.patch"))
			if !strings.Contains(patch, "+changed") || !strings.Contains(patch, "+new") {
				t.Fatalf("patch: %s", patch)
			}
			var want strings.Builder
			attempts := 1
			if status == "CRASH" {
				attempts = 2
			}
			for attempt := 0; attempt < attempts; attempt++ {
				attemptDir := record.WorkDir
				if attempt == 0 && attempts == 2 {
					lines := strings.Split(strings.TrimSpace(readFile(t, log)), "\n")
					fields := strings.Split(lines[0], "|")
					attemptDir = filepath.Dir(fields[2])
				}
				attemptClone, err := filepath.EvalSymlinks(filepath.Join(attemptDir, "repository"))
				if err != nil {
					t.Fatal(err)
				}
				for _, command := range []string{"register example", "implement example", "status example", "spend example --json"} {
					fmt.Fprintf(&want, "tasks %s|%s|%s|%s\n", command, attemptClone, filepath.Join(attemptDir, "data"), filepath.Join(attemptDir, "config"))
				}
			}
			if got := readFile(t, log); got != want.String() {
				t.Fatalf("commands:\n%s\nwant:\n%s", got, want.String())
			}
			configPath := filepath.Join(record.WorkDir, "config", "pop", "config.toml")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(record.WorkDir, "config"))
			cfg, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := cfg.ResolveRepoConfig(config.DefaultDeps(), filepath.Join(record.WorkDir, "repository"))
			if err != nil {
				t.Fatal(err)
			}
			if resolved.TurnCap != 0 {
				t.Fatalf("resolved config: %+v", resolved)
			}
			contents := readFile(t, configPath)
			for _, value := range []string{"max_tries = 3", "include_implementation_convention = true", "turn_cap = 0", `claude --model \"opus\"`} {
				if !strings.Contains(contents, value) {
					t.Fatalf("config missing %q: %s", value, contents)
				}
			}
			if !cfg.Work.Verify.Enabled || !cfg.Work.Refine.Enabled {
				t.Fatalf("phase config: %+v", cfg.Work)
			}
		})
	}
	for _, dir := range []string{human, humanConfig} {
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 1 || readFile(t, filepath.Join(dir, "sentinel")) != "untouched" {
			t.Fatalf("human directory changed: %v, %v", files, err)
		}
	}
	if got := readFile(t, filepath.Join(repo, "feature.txt")); got != "parent\n" {
		t.Fatalf("source changed: %s", got)
	}
}

func TestDeclaredPopArms(t *testing.T) {
	for _, name := range []string{"pop", "refine-off", "verify-off", "max-tries-1", "convention-off", "explore-off", "model-swap"} {
		arm, err := loadArm("arms", name)
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := writePopTrialConfig(root, "/trial/repository", arm); err != nil {
			t.Fatal(err)
		}
		var cfg config.Config
		if _, err := toml.DecodeFile(filepath.Join(root, "pop", "config.toml"), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Work.Verify.Enabled != (name != "verify-off") || cfg.Work.Refine.Enabled != (name != "refine-off") {
			t.Fatalf("%s: phase settings", name)
		}
	}
}
