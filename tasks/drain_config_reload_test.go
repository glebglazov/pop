package tasks

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestDrainEnablesRefineOnTheTurnAfterConfigChanges proves a running Drain does
// not keep the configuration snapshot it started with. The loader observes the
// first task's persisted completion and enables Refine for the next turn.
func TestDrainEnablesRefineOnTheTurnAfterConfigChanges(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	refinerRan := false

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.MaxTries = 1
	opts.MaxTriesExplicit = true
	opts.refineRunner = func(string) (string, string, error) {
		refinerRan = true
		return "## Fresh configuration\n\nRefine ran in this drain.", "claude", nil
	}
	loadConfig := func(string) (*config.Config, error) {
		m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
		enabled := false
		for _, task := range m.Tasks {
			if task.ID == "01-a" && task.Status == TaskDone {
				enabled = true
			}
		}
		return &config.Config{Work: &config.WorkConfig{
			Refine: &config.RefineConfig{Enabled: enabled},
		}}, nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone || !refinerRan {
		t.Fatalf("result = %+v, refiner ran = %v; want Refine in the same drain", result, refinerRan)
	}
}

// TestDrainHoldsTheLastGoodConfigUntilAReadRecovers drives a hold across two
// turns. Refine remains enabled from the last loadable snapshot, and the drain
// reports only the transitions into and out of the hold.
func TestDrainHoldsTheLastGoodConfigUntilAReadRecovers(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "03-h", File: "03-h.md", Title: "Sign off", Type: "HITL", Status: "open", BlockedBy: []string{"02-b"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	refinerRan := false
	failedReads := 0

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.MaxTries = 1
	opts.MaxTriesExplicit = true
	opts.ConfirmIn = strings.NewReader("2\n")
	opts.ConfirmOut = &buf
	opts.refineRunner = func(string) (string, string, error) {
		refinerRan = true
		return "## Held configuration\n\nRefine stayed enabled.", "claude", nil
	}
	loadConfig := func(string) (*config.Config, error) {
		m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
		doneAFK := 0
		hitlDone := false
		for _, task := range m.Tasks {
			if task.Type == "AFK" && task.Status == TaskDone {
				doneAFK++
			}
			if task.ID == "03-h" && task.Status == TaskDone {
				hitlDone = true
			}
		}
		if doneAFK > 0 && !hitlDone {
			failedReads++
			return nil, errors.New("parse broken TOML")
		}
		return &config.Config{Work: &config.WorkConfig{
			Refine: &config.RefineConfig{Enabled: true},
		}}, nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone || !refinerRan {
		t.Fatalf("result = %+v, refiner ran = %v; want the held Refine setting and a completed drain", result, refinerRan)
	}
	if failedReads < 2 {
		t.Fatalf("failed reads = %d, want a hold spanning at least two turns", failedReads)
	}
	out := buf.String()
	holdLine := "Configuration re-read failed for " + config.DefaultConfigPath() + "; keeping the previous configuration: load config: parse broken TOML"
	if got := strings.Count(out, holdLine); got != 1 {
		t.Fatalf("hold lines = %d, want 1 line %q; output:\n%s", got, holdLine, out)
	}
	recoveryLine := "Configuration re-read recovered for " + config.DefaultConfigPath()
	if got := strings.Count(out, recoveryLine); got != 1 {
		t.Fatalf("recovery lines = %d, want 1 line %q; output:\n%s", got, recoveryLine, out)
	}
}

// TestDrainTreatsMissingConfigAsNoConfiguration proves a removed file is a
// successful re-read of nil config. Built-in defaults apply on that turn and no
// failed-read hold starts.
func TestDrainTreatsMissingConfigAsNoConfiguration(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.MaxTries = 1
	opts.MaxTriesExplicit = true
	opts.refineRunner = func(string) (string, string, error) {
		t.Fatal("Refine must use the default-off setting after config disappears")
		return "", "", nil
	}
	loadConfig := func(string) (*config.Config, error) {
		m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
		for _, task := range m.Tasks {
			if task.ID == "01-a" && task.Status == TaskDone {
				return nil, os.ErrNotExist
			}
		}
		return &config.Config{Work: &config.WorkConfig{
			Refine: &config.RefineConfig{Enabled: true},
		}}, nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want DONE", result)
	}
	if strings.Contains(buf.String(), "Configuration re-read") {
		t.Fatalf("a missing config started a failed-read hold; output:\n%s", buf.String())
	}
}

// TestDrainKeepsImplementingAgentsFrozenWhenConfigChanges proves the fresh cfg
// snapshot does not replace the implementing agent list settled at run start.
func TestDrainKeepsImplementingAgentsFrozenWhenConfigChanges(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	const successfulAgent = `#!/bin/sh
TASK=$(cat "$(printf '%s' "$*" | sed -n 's|.*Read the file \([^ ]*\) in full:.*|\1|p' | head -1)" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\ndone\nSUMMARY_END\nTASK_COMPLETE\n'
`
	installAgentShim(t, env.root, "claude", successfulAgent)
	installAgentShim(t, env.root, "codex", successfulAgent)

	loadConfig := func(string) (*config.Config, error) {
		m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
		agent := "claude"
		for _, task := range m.Tasks {
			if task.ID == "01-a" && task.Status == TaskDone {
				agent = "codex"
			}
		}
		return &config.Config{Work: &config.WorkConfig{
			Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands(agent)},
		}}, nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.MaxTries = 1
	opts.MaxTriesExplicit = true
	result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want DONE", result)
	}
	if got := strings.Count(buf.String(), "Attempt 1/1 · claude"); got != 2 {
		t.Fatalf("claude attempts = %d, want 2; output:\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), "Attempt 1/1 · codex") {
		t.Fatalf("the changed implementing agent ran mid-drain; output:\n%s", buf.String())
	}
}
