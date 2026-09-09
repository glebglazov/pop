package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/ui"
)

// attendedPickFixture is a machine with two attended entries configured by hand
// and nothing overridden yet: the state a human sits in front of when the gate
// first offers them the choice.
type attendedPickFixture struct {
	d            *Deps
	cfg          *config.Config
	configPath   string
	overridePath string
}

func newAttendedPickFixture(t *testing.T, agents string) attendedPickFixture {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	configPath := filepath.Join(configDir, "pop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[work.attended]\nagents = " + agents + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			switch key {
			case "XDG_CONFIG_HOME":
				return configDir
			case "XDG_DATA_HOME":
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		WriteFileFunc:   os.WriteFile,
		MkdirAllFunc:    os.MkdirAll,
		RenameFunc:      os.Rename,
		RemoveAllFunc:   os.RemoveAll,
		StatFunc:        os.Stat,
	}
	d := &Deps{FS: fs, LookPath: func(file string) (string, error) { return "/usr/bin/" + file, nil }}
	cfg, err := config.LoadWith(&config.Deps{FS: fs}, configPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	return attendedPickFixture{d: d, cfg: cfg, configPath: configPath, overridePath: filepath.Join(dataDir, "pop", "config.override.toml")}
}

// gateKeyDriver stands in for the terminal: each call to the gate menu replays
// one round of keystrokes through the real component and answers with what it
// resolved, so a test presses tab and 1 rather than naming the result of doing
// so. Every frame is written to out, which is what the human would have seen.
func gateKeyDriver(t *testing.T, rounds ...[]tea.KeyPressMsg) (func(ui.GateMenuSpec, io.Reader, io.Writer, ui.GateMenuRunConfig) (ui.GateMenuResult, error), *[]ui.GateMenuSpec) {
	t.Helper()
	var seen []ui.GateMenuSpec
	round := 0
	return func(spec ui.GateMenuSpec, _ io.Reader, out io.Writer, _ ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		seen = append(seen, spec)
		if round >= len(rounds) {
			return ui.GateMenuResult{}, fmt.Errorf("gate menu opened %d times, only %d rounds scripted", round+1, len(rounds))
		}
		m := ui.NewGateMenu(spec)
		for _, key := range rounds[round] {
			m.Update(key)
		}
		round++
		fmt.Fprint(out, ui.StripANSI(m.ViewContent()))
		return ui.GateMenuResult{Key: m.Chosen(), PickAttended: m.PickedAttended(), PickAction: m.PickedAction()}, nil
	}, &seen
}

func pickerKeyDriver(key tea.KeyPressMsg) func([]ui.AttendedAgentEntry, io.Reader, io.Writer, func(string, ...any)) (*ui.AttendedAgentEntry, error) {
	return func(entries []ui.AttendedAgentEntry, _ io.Reader, _ io.Writer, _ func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		p := ui.NewAttendedAgentPicker(entries)
		p.Update(key)
		return p.Choice(), nil
	}
}

func gateSpecWithAssistRow() ui.GateMenuSpec {
	return ui.GateMenuSpec{
		Headline: "Human-blocked: demo/01-hitl",
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Get agent assistance (default)", Default: true, Assists: true},
			{Key: "0", Label: "Exit"},
		},
	}
}

// The keyboard path selects one whole entry for this run. It overrides the
// attended flag, but it changes neither source config nor an existing override.
func TestGatePickIsSessionOnlyAndLaunchesTheExactEntry(t *testing.T) {
	fx := newAttendedPickFixture(t, `[{ display_name = "Claude Usual", cmd = "claude --model opus" }, { display_name = "Claude Chosen", cmd = "claude --model sonnet --permission-mode plan" }]`)
	sourceBefore, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fx.overridePath), 0o755); err != nil {
		t.Fatal(err)
	}
	overrideBefore := []byte("[work.attended]\nagents = [\"codex\"]\n")
	if err := os.WriteFile(fx.overridePath, overrideBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	gate := NewAttendedSession(fx.cfg, "codex")

	driver, seen := gateKeyDriver(t,
		[]tea.KeyPressMsg{{Code: tea.KeyTab}},
		[]tea.KeyPressMsg{{Code: '1', Text: "1"}},
	)
	restore := swapGateSeams(t, driver, pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"}))
	defer restore()

	var out strings.Builder
	in := strings.NewReader("")
	key, forceQuit, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, gate)
	if err != nil || forceQuit {
		t.Fatalf("promptGateMenu = %q, forceQuit=%v, err=%v", key, forceQuit, err)
	}
	if key != "1" {
		t.Fatalf("key = %q, want 1", key)
	}

	if len(*seen) != 2 {
		t.Fatalf("the gate ran %d times, want 2 (before and after the pick)", len(*seen))
	}
	before, after := (*seen)[0], (*seen)[1]
	if before.AttendedLabel != "codex" || !before.AttendedPickable {
		t.Fatalf("the first row = %q pickable=%v", before.AttendedLabel, before.AttendedPickable)
	}
	if after.AttendedLabel != "Claude Chosen · sonnet" {
		t.Fatalf("the row after the pick = %q, want the picked entry", after.AttendedLabel)
	}
	if after.Notice != "" {
		t.Fatalf("a landed pick said %q", after.Notice)
	}
	if !strings.Contains(out.String(), "1. Get agent assistance (default) · Claude Chosen · sonnet · tab to change") {
		t.Fatalf("the re-rendered row is not what the human saw:\n%s", out.String())
	}

	// And the launch behind that row resolves the same entry, from the same
	// re-read config the row was rendered from.
	invocation, err := gate.ResolveAssistance(fx.d, "", "briefing", t.TempDir())
	if err != nil {
		t.Fatalf("ResolveAgentAssistanceInvocation: %v", err)
	}
	if invocation.AgentPreset != "claude" {
		t.Fatalf("the launch resolved %q, want the picked entry:\n%s", invocation.AgentPreset, invocation.Display)
	}
	wantArgs := []string{"--model", "sonnet", "--permission-mode", "plan", "briefing"}
	if !reflect.DeepEqual(invocation.Command.Args, wantArgs) {
		t.Fatalf("launch args = %#v, want %#v", invocation.Command.Args, wantArgs)
	}
	assertFileContent(t, fx.configPath, sourceBefore)
	assertFileContent(t, fx.overridePath, overrideBefore)

	other := NewAttendedSession(fx.cfg, "cursor")
	if got := other.EffectiveEntry().Cmd; got != "cursor" {
		t.Fatalf("another run inherited the choice: %q", got)
	}
	fresh := NewAttendedSession(fx.cfg, "")
	if got := fresh.EffectiveEntry().Cmd; got != "claude --model opus" {
		t.Fatalf("a fresh run inherited the choice: %q", got)
	}
}

// Escape from the chooser is not a pick: nothing is written, and the gate comes
// back exactly as it was.
func TestGatePickEscapeWritesNothing(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", "cursor"]`)
	gate := NewAttendedSession(fx.cfg, "codex")

	driver, seen := gateKeyDriver(t,
		[]tea.KeyPressMsg{{Code: tea.KeyTab}},
		[]tea.KeyPressMsg{{Code: '0', Text: "0"}},
	)
	restore := swapGateSeams(t, driver, pickerKeyDriver(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}))
	defer restore()

	var out strings.Builder
	in := strings.NewReader("")
	if _, _, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fx.overridePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape wrote an override: %v", err)
	}
	if after := (*seen)[1]; after.AttendedLabel != "codex" || after.Notice != "" {
		t.Fatalf("the gate came back changed: row %q notice %q", after.AttendedLabel, after.Notice)
	}
	invocation, err := gate.ResolveAssistance(fx.d, "", "briefing", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "codex" {
		t.Fatalf("cancel changed the attended flag: %q", invocation.AgentPreset)
	}
}

// A picker failure leaves the entry already in force and reports the failure in
// the gate instead of changing session state.
func TestGatePickerErrorKeepsTheEntryInForce(t *testing.T) {
	fx := newAttendedPickFixture(t, `[{ display_name = "Claude Usual", cmd = "claude --model opus" }, { display_name = "Cursor", cmd = "cursor" }]`)
	gate := NewAttendedSession(fx.cfg, "codex")

	driver, seen := gateKeyDriver(t,
		[]tea.KeyPressMsg{{Code: tea.KeyTab}},
		[]tea.KeyPressMsg{{Code: '1', Text: "1"}},
	)
	restore := swapGateSeams(t, driver, func([]ui.AttendedAgentEntry, io.Reader, io.Writer, func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		return nil, errors.New("picker broke")
	})
	defer restore()

	var out strings.Builder
	in := strings.NewReader("")
	if _, _, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, gate); err != nil {
		t.Fatal(err)
	}
	after := (*seen)[1]
	if after.AttendedLabel != "codex" {
		t.Fatalf("the row = %q, want the entry that was in force", after.AttendedLabel)
	}
	if !strings.Contains(after.Notice, "picker broke") {
		t.Fatalf("the refusal is not in the menu: %q", after.Notice)
	}
	if !strings.Contains(out.String(), "picker broke") {
		t.Fatalf("the human never saw the refusal:\n%s", out.String())
	}
}

func TestDrainKeepsItsAttendedChoiceAcrossGateEnvironments(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", "cursor"]`)
	run := &implementRun{
		d:        fx.d,
		plan:     &runPlan{cfg: fx.cfg},
		resolved: &ResolvedPaths{},
		// This is the implementation agent flag. It must not select attended
		// assistance before the human uses Tab.
		opts: RunTaskSetOptions{AgentPreset: "codex"},
	}
	first := run.newGateEnv().cfg
	if got := first.EffectiveEntry().Cmd; got != "claude --model opus" {
		t.Fatalf("implementation flag became attended choice: %q", got)
	}

	original := runAttendedPicker
	runAttendedPicker = pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"})
	t.Cleanup(func() { runAttendedPicker = original })
	if notice := first.Pick(strings.NewReader(""), io.Discard, func(string, ...any) {}); notice != "" {
		t.Fatalf("pick notice = %q", notice)
	}
	second := run.newGateEnv().cfg
	if first != second || second.EffectiveEntry().Cmd != "cursor" {
		t.Fatalf("later gate lost choice: first=%p second=%p entry=%q", first, second, second.EffectiveEntry().Cmd)
	}

	runAttendedPicker = func([]ui.AttendedAgentEntry, io.Reader, io.Writer, func(string, ...any)) (*ui.AttendedAgentEntry, error) {
		return nil, errors.New("picker broke later")
	}
	second.Pick(strings.NewReader(""), io.Discard, func(string, ...any) {})
	if got := second.EffectiveEntry().Cmd; got != "cursor" {
		t.Fatalf("picker error lost current choice: %q", got)
	}

	fresh := (&implementRun{d: fx.d, plan: &runPlan{cfg: fx.cfg}, resolved: &ResolvedPaths{}}).newGateEnv().cfg
	if got := fresh.EffectiveEntry().Cmd; got != "claude --model opus" {
		t.Fatalf("fresh drain inherited choice: %q", got)
	}
}

// A headless gate has no chooser to open, so its row advertises none and the
// word is just another unlisted answer to the line reader (ADR-0264 decision 7).
func TestGatePickAbsentFromTheLineReaderPath(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", "cursor"]`)
	var out strings.Builder
	in := strings.NewReader("tab\n0\n")
	key, _, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, NewAttendedSession(fx.cfg, ""))
	if err != nil {
		t.Fatal(err)
	}
	if key != "0" {
		t.Fatalf("key = %q, want 0", key)
	}
	if !strings.Contains(out.String(), "Choose 1, or 0.") {
		t.Fatalf("tab should have been re-prompted:\n%s", out.String())
	}
	if strings.Contains(out.String(), "tab to change") {
		t.Fatalf("the line path advertised a chooser it cannot open:\n%s", out.String())
	}
	if !strings.Contains(ui.StripANSI(out.String()), "1. Get agent assistance (default) · claude --model opus · opus\n") {
		t.Fatalf("the row is not what a headless gate has always printed:\n%s", out.String())
	}
}

// A malformed entry launches nothing, so it is never offered as something to
// launch — and it is not what the pick writes back either.
func TestAttendedPickChoicesSkipMalformedEntries(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", { display_name = "Broken" }, "cursor", "codex"]`)
	choices := AttendedPickChoices(fx.cfg)
	if len(choices) != 3 {
		t.Fatalf("choices = %+v, want the three usable entries", choices)
	}
	for _, choice := range choices {
		if strings.Contains(choice.Label, "Broken") || strings.Contains(choice.Label, "malformed") {
			t.Fatalf("a malformed entry was offered: %+v", choice)
		}
	}
}

// One usable entry is no choice: the row says what it has always said and the
// key opens nothing.
func TestAttendedPickNotOfferedForASingleEntry(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus"]`)
	if AttendedPickOffered(fx.cfg) {
		t.Fatal("a list of one offers a choice")
	}
	if AttendedPickOffered(nil) {
		t.Fatal("an unconfigured machine offers a choice")
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s changed:\n%s", path, got)
	}
}

func swapGateSeams(t *testing.T, menu func(ui.GateMenuSpec, io.Reader, io.Writer, ui.GateMenuRunConfig) (ui.GateMenuResult, error), picker func([]ui.AttendedAgentEntry, io.Reader, io.Writer, func(string, ...any)) (*ui.AttendedAgentEntry, error)) func() {
	t.Helper()
	origMenu, origPicker := runGateMenu, runAttendedPicker
	runGateMenu, runAttendedPicker = menu, picker
	return func() { runGateMenu, runAttendedPicker = origMenu, origPicker }
}
