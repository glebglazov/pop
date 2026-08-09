package tasks

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

func TestPromptGateMenuPrintsAttendedEntry(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("0\n")
	d := &Deps{}
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	spec := ui.GateMenuSpec{
		Headline: "Human-blocked: demo/01-hitl",
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Get agent assistance (default)", Default: true, Assists: true},
			{Key: "0", Label: "Exit"},
		},
	}
	key, _, err := promptGateMenu(&out, in, newPromptReader(in), spec, nil, d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if key != "0" {
		t.Fatalf("key = %q, want 0", key)
	}
	want := "1. Get agent assistance (default) · Claude Usual · opus"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("menu missing attended render %q:\n%s", want, out.String())
	}
}

func TestPromptGateMenuOverrideSurvivesReresolve(t *testing.T) {
	d := &Deps{}
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{DisplayName: "Cursor", Cmd: "cursor"},
		}},
	}}

	origMenu := runGateMenu
	origPicker := runAgentOverridePicker
	defer func() {
		runGateMenu = origMenu
		runAgentOverridePicker = origPicker
	}()

	calls := 0
	runGateMenu = func(spec ui.GateMenuSpec, in io.Reader, out io.Writer, cfg ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		calls++
		switch calls {
		case 1:
			if !strings.Contains(spec.AttendedLabel, "Claude Usual") {
				t.Fatalf("first label = %q", spec.AttendedLabel)
			}
			return ui.GateMenuResult{OpenOverride: true}, nil
		case 2:
			if !strings.Contains(spec.AttendedLabel, "Cursor") {
				t.Fatalf("after override label = %q, want Cursor", spec.AttendedLabel)
			}
			return ui.GateMenuResult{Key: "0"}, nil
		default:
			t.Fatalf("unexpected gate menu call %d", calls)
			return ui.GateMenuResult{}, nil
		}
	}
	runAgentOverridePicker = func(groups []ui.AgentOverrideGroup, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AgentOverrideChoice, error) {
		return &ui.AgentOverrideChoice{Group: "attended", Cmd: "cursor"}, nil
	}

	var out bytes.Buffer
	in := strings.NewReader("")
	_, _, err := promptGateMenu(&out, in, newPromptReader(in), ui.GateMenuSpec{
		Items: []ui.GateMenuItem{{Key: "0", Label: "Exit"}},
	}, nil, d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d.AgentOverrides == nil || d.AgentOverrides.AttendedCmd() != "cursor" {
		t.Fatalf("override bag = %#v, want attended=cursor", d.AgentOverrides)
	}

	// Re-resolution after a side-trip must see the same promotion on the same Deps.
	d2 := attendedTestDeps(t)
	d2.AgentOverrides = d.AgentOverrides
	invocation, err := ResolveAgentAssistanceInvocation(d2, cfg, "", "", "prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "cursor" {
		t.Fatalf("re-resolve preset = %q, want cursor (got display %q)", invocation.AgentPreset, invocation.Display)
	}
}

func TestPromptGateMenuNonPromptableUnaffected(t *testing.T) {
	// canPrompt false paths never call promptGateMenu; this pins the menu seam
	// itself does not invent a picker on the line path when OpenOverride is off.
	var out bytes.Buffer
	in := strings.NewReader("0\n")
	origPicker := runAgentOverridePicker
	defer func() { runAgentOverridePicker = origPicker }()
	pickerCalls := 0
	runAgentOverridePicker = func(groups []ui.AgentOverrideGroup, in io.Reader, out io.Writer, warn func(string, ...any)) (*ui.AgentOverrideChoice, error) {
		pickerCalls++
		return nil, nil
	}
	_, _, err := promptGateMenu(&out, in, newPromptReader(in), ui.GateMenuSpec{
		Items: []ui.GateMenuItem{{Key: "0", Label: "Exit"}},
	}, nil, &Deps{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pickerCalls != 0 {
		t.Fatalf("picker called %d times on a plain digit choice", pickerCalls)
	}
}
