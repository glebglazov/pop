package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// The agent process is the only double: path resolution, claims, report writes,
// and the Refine commit all run against the real checkout and Work store.
type assistRefineRunner struct {
	*scriptedVerifyRunner
	before func(string, []string)
}

func (r *assistRefineRunner) Start(ctx context.Context, dir string, out, errOut io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.before(dir, args)
	return r.scriptedVerifyRunner.Start(ctx, dir, out, errOut, name, args...)
}

func TestAssistPhaseChoicesReachManualPassesAndEndWithSession(t *testing.T) {
	env := setupRefineCommitFixture(t, houseConvention)
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	d.LookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	cfg := instantVerifyRetryConfig(1)
	cfg.Work.Refine = &config.RefineConfig{Agents: config.AgentEntriesFromCommands("claude --model opus", "claude --model sonnet --permission-mode acceptEdits"), Effort: "heavy"}
	cfg.Work.Verify.Agents = config.AgentEntriesFromCommands("claude --model opus", "claude --model haiku")
	cfg.Work.Attended = &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("codex", "cursor")}
	runner := &scriptedVerifyRunner{scripts: []string{
		claudeRefineStream("REFINE-OUTCOME: refined\\nCOMMIT-SUBJECT: " + refinedSubject + "\\n\\n## Fixed\\nRefined the file."),
		claudeRefineStream("VERDICT: PASS"),
	}}
	d.Runner = &probeNeutralRunner{inner: &assistRefineRunner{scriptedVerifyRunner: runner, before: func(dir string, args []string) {
		root, _ := filepath.EvalSymlinks(env.root)
		if dir != root {
			t.Fatalf("runtime=%s", dir)
		}
		claim, err := ReadCheckoutClaim(d, dir)
		if err != nil || claim == nil {
			t.Fatalf("phase has no claim: %+v %v", claim, err)
		}
		if runner.calls == 0 {
			prompt := capturedPrompt(args)
			for _, want := range []string{"ASSIST-IMPLEMENTATION", "ASSIST-OVERLAY", houseConvention} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt missing %s", want)
				}
			}
			writeTaskMD(t, dir, "refined.md", "refined content\n")
		}
	}}}
	oldMenu, oldPicker := runGateMenu, runAttendedPicker
	t.Cleanup(func() { runGateMenu, runAttendedPicker = oldMenu, oldPicker })
	// Select each role, cancel a second Refine pick, then run both phases.
	actions := []string{"pick-assist", "pick-verify", "pick-refine", "pick-refine", "f", "verify", "0"}
	round, picks := 0, 0
	runAttendedPicker = func(entries []ui.AttendedAgentEntry, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		if runner.calls != 0 {
			t.Fatal("selection ran a phase")
		}
		picks++
		if picks == 4 {
			return pickerKeyDriver(tea.KeyPressMsg{Code: tea.KeyEscape})(entries, in, out, warn)
		}
		return pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"})(entries, in, out, warn)
	}
	runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, opts ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		if round >= len(actions) {
			t.Fatal("unexpected menu")
		}
		action := actions[round]
		round++
		var target string
		for _, item := range spec.Items {
			if item.Role == "refine" && round >= 4 && !strings.Contains(item.AgentLabel, "sonnet") {
				t.Fatalf("refiner=%s", item.AgentLabel)
			}
			if item.Role == "verify" && round >= 3 && !strings.Contains(item.AgentLabel, "haiku") {
				t.Fatalf("verifier=%s", item.AgentLabel)
			}
			if (action == "pick-assist" && item.Assists) || (strings.TrimPrefix(action, "pick-") == item.Role && item.Role != "") {
				target = item.Key
			}
		}
		if round >= 2 && !strings.Contains(spec.AttendedLabel, "cursor") {
			t.Fatalf("attended=%s", spec.AttendedLabel)
		}
		if runner.calls == 1 {
			m := LoadManifest(d, "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
			if _, ok := latestRefinePointer(d, m); !ok {
				t.Fatal("missing refreshed report")
			}
			if _, ok := latestVerifyPointer(d, m); ok {
				t.Fatal("Refine ran Verify")
			}
			found := false
			for _, item := range spec.Items {
				if strings.Contains(item.Label, "Read the refine report") {
					found = true
				}
			}
			if !found {
				t.Fatal("report action not refreshed")
			}
		}
		var keys []tea.KeyPressMsg
		if strings.HasPrefix(action, "pick-") {
			// Drive the real menu keyboard from its initial row.
			spec.FocusKey = ""
			for _, item := range spec.Items {
				if item.Key == target {
					break
				}
				keys = append(keys, tea.KeyPressMsg{Code: tea.KeyDown})
			}
			keys = append(keys, tea.KeyPressMsg{Code: tea.KeyTab})
		} else {
			if action == "verify" {
				action = target
			}
			keys = []tea.KeyPressMsg{{Code: rune(action[0]), Text: action}}
		}
		drive, _ := gateKeyDriver(t, keys)
		return drive(spec, in, out, opts)
	}
	manifestPath := filepath.Join(env.tasksDir, "demo", "index.json")
	before, _ := os.ReadFile(manifestPath)
	var out bytes.Buffer
	options := AssistOptions{TaskSetID: "demo", ResolveInput: ResolveInput{CWD: env.root, RuntimeOverride: env.root, DefinitionOverride: env.tasksDir}, Input: strings.NewReader(""), Output: &out, AgentPreset: "claude", RefineOptions: RefineOptions{
		Agents: []string{"cursor"}, Effort: "light",
		Convention: func(string) (string, error) { return "ASSIST-IMPLEMENTATION", nil },
		Overlay:    func(string) (string, error) { return "ASSIST-OVERLAY", nil },
	}}
	load := func(string) (*config.Config, error) { return cfg, nil }
	if err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, load, options); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatalf("calls=%d\n%s", runner.calls, out.String())
	}
	for i, want := range []string{"--model sonnet --permission-mode acceptEdits", "--model haiku"} {
		if !strings.Contains(strings.Join(runner.args[i], " "), want) {
			t.Fatalf("launch=%v", runner.args[i])
		}
	}
	if headSubject(t, env.root) != refinedSubject || readRefineTrailer(t, env.root, "HEAD") != "demo" {
		t.Fatal("manual Refine did not commit")
	}
	after, _ := os.ReadFile(manifestPath)
	if !bytes.Equal(before, after) {
		t.Fatal("phase changed task state")
	}
	if claim, err := ReadCheckoutClaim(d, env.root); err != nil || claim != nil {
		t.Fatalf("claim=%+v %v", claim, err)
	}
	// A separate invocation in the same checkout starts from flags and config.
	runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, opts ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		if !strings.Contains(spec.AttendedLabel, "claude") {
			t.Fatal("attended choice leaked")
		}
		for _, item := range spec.Items {
			if item.Role == "refine" && !strings.Contains(item.AgentLabel, "cursor") {
				t.Fatal("refine choice leaked")
			}
			if item.Role == "verify" && !strings.Contains(item.AgentLabel, "opus") {
				t.Fatal("verify choice leaked")
			}
		}
		return ui.GateMenuResult{Key: "0"}, nil
	}
	if err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, load, options); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatal("opening a session ran a phase")
	}
}

func TestAssistRefineRefusalReturnsToMenuWithoutFallback(t *testing.T) {
	for _, kind := range []string{"missing", "cooldown", "failure", "no finished work"} {
		t.Run(kind, func(t *testing.T) {
			tasks := signOffSet()
			if kind == "no finished work" {
				tasks[0].Status = TaskOpen
			}
			env := setupDrainRefineFixture(t, tasks)
			d := env.deps()
			d.LookPath = func(name string) (string, error) {
				if kind == "missing" && name == "codex" {
					return "", errors.New("missing")
				}
				return "/bin/" + name, nil
			}
			cfg := &config.Config{Work: &config.WorkConfig{Refine: &config.RefineConfig{Agents: config.AgentEntriesFromCommands("claude", "codex")}}}
			runner := &scriptedVerifyRunner{exitCode: 1}
			if kind == "cooldown" {
				_, err := recordAgentQuotaCooldown(d, AgentQuotaCooldownRequest{Preset: "codex", Stated: time.Now().Add(time.Hour)}, time.Now(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
			}
			d.Runner = &probeNeutralRunner{inner: runner}
			oldMenu, oldPicker := runGateMenu, runAttendedPicker
			t.Cleanup(func() { runGateMenu, runAttendedPicker = oldMenu, oldPicker })
			rounds := 0
			runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, opts ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
				rounds++
				switch rounds {
				case 1:
					return ui.GateMenuResult{PickAction: "f"}, nil
				case 2:
					return ui.GateMenuResult{Key: "f"}, nil
				default:
					return ui.GateMenuResult{Key: "0"}, nil
				}
			}
			runAttendedPicker = pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"})
			var out bytes.Buffer
			err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) { return cfg, nil }, AssistOptions{TaskSetID: "demo", ResolveInput: ResolveInput{CWD: env.root, RuntimeOverride: env.root, DefinitionOverride: env.tasksDir}, Input: strings.NewReader(""), Output: &out})
			wantCalls := 0
			if kind == "failure" {
				wantCalls = config.DefaultTaskMaxTries
			}
			for _, name := range runner.names {
				if name != "codex" {
					t.Fatalf("substituted %s", name)
				}
			}
			if err != nil || rounds != 3 || runner.calls != wantCalls || !strings.Contains(out.String(), "Could not refine") {
				t.Fatalf("err=%v rounds=%d calls=%d\n%s", err, rounds, runner.calls, out.String())
			}
		})
	}
}
