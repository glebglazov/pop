package tasks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	cfg, err := config.LoadWith(configDeps(d), configPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	return attendedPickFixture{d: d, cfg: cfg, overridePath: filepath.Join(dataDir, "pop", "config.override.toml")}
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
		return ui.GateMenuResult{Key: m.Chosen(), PickAttended: m.PickedAttended()}, nil
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

// The whole path ADR-0264 describes, driven from the keyboard: a gate whose
// assist row names the head entry, tab, a pick of the second entry, and back to
// the gate — where the row names what a re-read of config resolves, the override
// layer holds the reordered list, and 1 launches the entry the row promised.
func TestGatePickWritesTheOverrideAndLaunchesThePickedEntry(t *testing.T) {
	fx := newAttendedPickFixture(t, `[{ display_name = "Claude Usual", cmd = "claude --model opus" }, { display_name = "Cursor", cmd = "cursor" }]`)
	gate := newGateConfig(fx.d, fx.cfg)

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

	assertStoredOrder(t, fx.overridePath, `"cursor"`, `"claude --model opus"`)

	if len(*seen) != 2 {
		t.Fatalf("the gate ran %d times, want 2 (before and after the pick)", len(*seen))
	}
	before, after := (*seen)[0], (*seen)[1]
	if before.AttendedLabel != "Claude Usual · opus" || !before.AttendedPickable {
		t.Fatalf("the first row = %q pickable=%v", before.AttendedLabel, before.AttendedPickable)
	}
	if after.AttendedLabel != "Cursor" {
		t.Fatalf("the row after the pick = %q, want the picked entry", after.AttendedLabel)
	}
	if after.Notice != "" {
		t.Fatalf("a landed pick said %q", after.Notice)
	}
	if !strings.Contains(out.String(), "1. Get agent assistance (default) · Cursor · tab to change") {
		t.Fatalf("the re-rendered row is not what the human saw:\n%s", out.String())
	}

	// And the launch behind that row resolves the same entry, from the same
	// re-read config the row was rendered from.
	invocation, err := ResolveAgentAssistanceInvocation(fx.d, gate.Value(), "", "", "briefing", t.TempDir())
	if err != nil {
		t.Fatalf("ResolveAgentAssistanceInvocation: %v", err)
	}
	if invocation.AgentPreset != "cursor" {
		t.Fatalf("the launch resolved %q, want the picked entry:\n%s", invocation.AgentPreset, invocation.Display)
	}
}

// Escape from the chooser is not a pick: nothing is written, and the gate comes
// back exactly as it was.
func TestGatePickEscapeWritesNothing(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", "cursor"]`)
	gate := newGateConfig(fx.d, fx.cfg)

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
	if after := (*seen)[1]; after.AttendedLabel != "claude --model opus · opus" || after.Notice != "" {
		t.Fatalf("the gate came back changed: row %q notice %q", after.AttendedLabel, after.Notice)
	}
}

// A write the layer refuses leaves the row naming the entry that was in force,
// and says why in the menu — the gate's stdout is the drain log.
func TestGatePickRefusedWriteKeepsTheEntryInForce(t *testing.T) {
	fx := newAttendedPickFixture(t, `[{ display_name = "Claude Usual", cmd = "claude --model opus" }, { display_name = "Cursor", cmd = "cursor" }]`)
	mock := fx.d.FS.(*deps.MockFileSystem)
	mock.WriteFileFunc = func(string, []byte, os.FileMode) error {
		return errors.New("the override file is read-only")
	}
	gate := newGateConfig(fx.d, fx.cfg)

	driver, seen := gateKeyDriver(t,
		[]tea.KeyPressMsg{{Code: tea.KeyTab}},
		[]tea.KeyPressMsg{{Code: '1', Text: "1"}},
	)
	restore := swapGateSeams(t, driver, pickerKeyDriver(tea.KeyPressMsg{Code: '2', Text: "2"}))
	defer restore()

	var out strings.Builder
	in := strings.NewReader("")
	if _, _, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, gate); err != nil {
		t.Fatal(err)
	}
	after := (*seen)[1]
	if after.AttendedLabel != "Claude Usual · opus" {
		t.Fatalf("the row = %q, want the entry that was in force", after.AttendedLabel)
	}
	if !strings.Contains(after.Notice, "the override file is read-only") {
		t.Fatalf("the refusal is not in the menu: %q", after.Notice)
	}
	if !strings.Contains(out.String(), "the override file is read-only") {
		t.Fatalf("the human never saw the refusal:\n%s", out.String())
	}
}

// A headless gate has no chooser to open, so its row advertises none and the
// word is just another unlisted answer to the line reader (ADR-0264 decision 7).
func TestGatePickAbsentFromTheLineReaderPath(t *testing.T) {
	fx := newAttendedPickFixture(t, `["claude --model opus", "cursor"]`)
	var out strings.Builder
	in := strings.NewReader("tab\n0\n")
	key, _, err := promptGateMenu(&out, in, newPromptReader(in), gateSpecWithAssistRow(), nil, newGateConfig(fx.d, fx.cfg))
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
	if err := PromoteAttendedAgent(fx.d, fx.cfg, "cursor"); err != nil {
		t.Fatalf("PromoteAttendedAgent: %v", err)
	}
	// The head moves and the tail keeps the order it was configured in.
	assertStoredOrder(t, fx.overridePath, `"cursor"`, `"claude --model opus"`)
	assertStoredOrder(t, fx.overridePath, `"claude --model opus"`, `"codex"`)
	stored, err := os.ReadFile(fx.overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "Broken") {
		t.Fatalf("the malformed entry was written back:\n%s", stored)
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

// assertStoredOrder reads the override layer as the file it is and checks the
// attended list runs head-first in the order a pick left it.
func assertStoredOrder(t *testing.T, path string, head, tail string) {
	t.Helper()
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the override layer: %v", err)
	}
	headAt, tailAt := strings.Index(string(stored), head), strings.Index(string(stored), tail)
	if headAt < 0 || tailAt < 0 || headAt > tailAt {
		t.Fatalf("the override does not hold %s ahead of %s:\n%s", head, tail, stored)
	}
}

func swapGateSeams(t *testing.T, menu func(ui.GateMenuSpec, io.Reader, io.Writer, ui.GateMenuRunConfig) (ui.GateMenuResult, error), picker func([]ui.AttendedAgentEntry, io.Reader, io.Writer, func(string, ...any)) (*ui.AttendedAgentEntry, error)) func() {
	t.Helper()
	origMenu, origPicker := runGateMenu, runAttendedPicker
	runGateMenu, runAttendedPicker = menu, picker
	return func() { runGateMenu, runAttendedPicker = origMenu, origPicker }
}
