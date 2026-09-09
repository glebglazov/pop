package tasks

import (
	"bytes"
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
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/ui"
)

func TestAssistVerifyChoiceRunsFreshAndStaysSeparate(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, append(doneAFKSet(), Task{ID: "02-hitl", File: "02-hitl.md", Title: "Sign off", Type: "HITL", Status: TaskOpen}))
	cfg := instantVerifyRetryConfig(1)
	cfg.Work.Verify.Enabled = true
	cfg.Work.Verify.Agents = config.AgentEntries{{DisplayName: "First verifier", Cmd: "claude --model opus"}, {DisplayName: "Chosen verifier", Cmd: "claude --model sonnet --permission-mode plan"}}
	var malformed config.AgentEntries
	if err := malformed.UnmarshalTOML([]any{""}); err != nil {
		t.Fatal(err)
	}
	cfg.Work.Verify.Agents = append(cfg.Work.Verify.Agents, malformed...)
	cfg.Work.Attended = &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("codex", "cursor")}
	runner := &scriptedVerifyRunner{scripts: []string{
		"{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"VERDICT: FIXABLE\\nFINDINGS: fresh problem\"}",
		"{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"VERDICT: PASS\"}",
	}}
	d.Runner = &probeNeutralRunner{inner: runner}
	d.LookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaASSIST", Verdict: "PASS", Findings: "old"})
	path := filepath.Join(defPath, "demo", "index.json")
	before, _ := os.ReadFile(path)
	oldMenu, oldPicker := runGateMenu, runAttendedPicker
	t.Cleanup(func() { runGateMenu, runAttendedPicker = oldMenu, oldPicker })
	key := func(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	runGateMenu, _ = gateKeyDriver(t,
		[]tea.KeyPressMsg{down, down, down, down, tab},
		[]tea.KeyPressMsg{tea.KeyPressMsg{Code: tea.KeyEnter}},
		[]tea.KeyPressMsg{key("5")},
		[]tea.KeyPressMsg{key("0")},
	)
	runAttendedPicker = func(entries []ui.AttendedAgentEntry, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		if runner.calls != 0 {
			t.Fatal("Tab ran a phase")
		}
		if len(entries) != 2 || !strings.Contains(entries[1].Label, "Chosen verifier") {
			t.Fatalf("choices = %+v", entries)
		}
		return pickerKeyDriver(key("2"))(entries, in, out, warn)
	}
	var out bytes.Buffer
	err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) { return cfg, nil }, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, RuntimeOverride: root, DefinitionOverride: defPath}, TaskSetID: "demo", Input: strings.NewReader(""), Output: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatalf("calls=%d\n%s", runner.calls, out.String())
	}
	for _, args := range runner.args {
		cmd := strings.Join(args, " ")
		if !strings.Contains(cmd, "--model sonnet") || !strings.Contains(cmd, "--permission-mode plan") {
			t.Fatalf("launch=%s", cmd)
		}
	}
	if !strings.Contains(out.String(), "Chosen verifier · sonnet") || !strings.Contains(out.String(), "Verify-failed:") || !strings.Contains(out.String(), "codex") {
		t.Fatalf("menu=%s", out.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("Verify changed the manifest")
	}
	v := readStoredVerdict(t, d, "/repo/.git", "demo", "shaASSIST")
	if v == nil || v.Verdict != "PASS" || v.HumanAuthored {
		t.Fatalf("verdict=%+v", v)
	}
	m := LoadManifest(d, "demo", path)
	if BlockingHITLTask(m) == nil {
		t.Fatal("Verify completed HITL")
	}
	if _, ok := latestVerifyPointer(d, m); !ok {
		t.Fatal("no report")
	}
	if claim, err := ReadCheckoutClaim(d, root); err != nil || claim != nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	// A new Assist invocation opens without a phase or a previous choice.
	runGateMenu, _ = gateKeyDriver(t, []tea.KeyPressMsg{key("0")})
	out.Reset()
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) { return cfg, nil }, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, RuntimeOverride: root, DefinitionOverride: defPath}, TaskSetID: "demo", Input: strings.NewReader(""), Output: &out,
	})
	if err != nil || runner.calls != 2 || !strings.Contains(out.String(), "First verifier") {
		t.Fatalf("new session: %v\n%s", err, out.String())
	}
}

func TestVerifyPickerOverridesFlagsAndManifestAndCancelPreservesChoice(t *testing.T) {
	cfg := instantVerifyRetryConfig(1)
	cfg.Work.Verify.Agents = config.AgentEntriesFromCommands("cursor")
	m := manifestWithVerifier(t, []string{"claude --model opus", "claude --model sonnet"}, "heavy")
	m.Valid = true
	session := NewAttendedSession(cfg, "codex")
	env := gateEnv{cfg: session}
	env.bindVerify(m, &reverifyGateContext{cfg: cfg, agents: []string{"cursor"}, effort: "light"})
	oldMenu, oldPicker := runGateMenu, runAttendedPicker
	t.Cleanup(func() { runGateMenu, runAttendedPicker = oldMenu, oldPicker })
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	runGateMenu, _ = gateKeyDriver(t, []tea.KeyPressMsg{tab}, []tea.KeyPressMsg{tab}, []tea.KeyPressMsg{{Code: tea.KeyEnter}})
	picks := 0
	runAttendedPicker = func(entries []ui.AttendedAgentEntry, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		picks++
		if len(entries) != 2 || entries[1].Cmd != "claude --model sonnet" {
			t.Fatalf("choices=%+v", entries)
		}
		if picks == 2 {
			return pickerKeyDriver(tea.KeyPressMsg{Code: tea.KeyEscape})(entries, in, out, warn)
		}
		return pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"})(entries, in, out, warn)
	}
	spec := ui.GateMenuSpec{Items: []ui.GateMenuItem{{Key: "v", Label: "Verify", Role: "verify", Default: true}, {Key: "0", Label: "Exit"}}}
	key, _, err := promptGateMenu(io.Discard, strings.NewReader(""), nil, spec, nil, session)
	if err != nil || key != "v" || session.verifyChoice == nil || session.verifyChoice.Cmd != "claude --model sonnet" {
		t.Fatalf("choice=%+v key=%s err=%v", session.verifyChoice, key, err)
	}
	if session.EffectiveEntry().Cmd != "codex" {
		t.Fatal("Verify changed attended choice")
	}
	for _, kind := range []string{"missing", "cooldown", "failure"} {
		t.Run(kind, func(t *testing.T) {
			d, def := setupVerifyFixture(t, stubGit("sha\n", "", ""))
			d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
			runner := &scriptedVerifyRunner{scripts: []string{""}, exitCode: 1}
			d.Runner = &probeNeutralRunner{inner: runner}
			d.LookPath = func(name string) (string, error) {
				if kind == "missing" && name == "claude" {
					return "", errors.New("missing")
				}
				return "/bin/" + name, nil
			}
			if kind == "cooldown" {
				_, err := recordAgentQuotaCooldown(d, AgentQuotaCooldownRequest{Preset: "claude", Stated: time.Now().Add(time.Hour)}, time.Now(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, _ = verifyResolvedSet(d, cfg, verifyCoreOptions{Repo: "/repo/.git", DefPath: def, RuntimePath: t.TempDir(), SetID: "demo", Agents: []string{"cursor"}, PhaseChoice: session.verifyChoice, Output: io.Discard})
			want := 0
			if kind == "failure" {
				want = 1
			}
			if runner.calls != want {
				t.Fatalf("calls=%d want=%d", runner.calls, want)
			}
			for _, name := range runner.names {
				if name != "claude" {
					t.Fatalf("substituted %s", name)
				}
			}
		})
	}
}

func TestAssistUnavailableVerifyReturnsToGateWithoutStartingAnotherEntry(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	cfg := instantVerifyRetryConfig(1)
	cfg.Work.Verify.Agents = config.AgentEntriesFromCommands("claude", "codex")
	d.LookPath = func(name string) (string, error) {
		if name == "codex" {
			return "", errors.New("not installed")
		}
		return "/bin/" + name, nil
	}
	runner := &scriptedVerifyRunner{}
	d.Runner = &probeNeutralRunner{inner: runner}
	oldMenu, oldPicker := runGateMenu, runAttendedPicker
	t.Cleanup(func() { runGateMenu, runAttendedPicker = oldMenu, oldPicker })
	runGateMenu, _ = gateKeyDriver(t,
		[]tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyTab}},
		[]tea.KeyPressMsg{{Code: tea.KeyEnter}},
		[]tea.KeyPressMsg{{Code: '0', Text: "0"}},
	)
	runAttendedPicker = pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"})
	var out bytes.Buffer
	err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) { return cfg, nil }, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, RuntimeOverride: root, DefinitionOverride: defPath},
		TaskSetID:    "demo", Input: strings.NewReader(""), Output: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 || !strings.Contains(out.String(), "Could not re-verify") || strings.Count(out.String(), "Assist: demo") != 3 {
		t.Fatalf("calls=%d\n%s", runner.calls, out.String())
	}
}
