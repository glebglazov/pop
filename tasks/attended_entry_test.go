package tasks

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/ui"
)

// Inline renders drop the model clause entirely when the entry pins none; only
// the catalog's own column says who decides.
func TestFormatAgentEntryNamesOnlyTheEntryWhenNoModel(t *testing.T) {
	e := AgentGroupEntry{DisplayName: "Cursor Usual", Cmd: "cursor"}
	if got := FormatAgentEntry(e); got != "Cursor Usual" {
		t.Fatalf("FormatAgentEntry = %q, want %q", got, "Cursor Usual")
	}
	if got := e.ModelLabel(); got != AgentEntryNoModelLabel {
		t.Fatalf("ModelLabel = %q, want %q", got, AgentEntryNoModelLabel)
	}
}

func TestEffectiveAttendedEntryTakesTheHeadOfTheMergedList(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Cursor", Cmd: "cursor"},
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	got := EffectiveAttendedEntry(cfg)
	if got.Cmd != "cursor" {
		t.Fatalf("cmd = %q, want cursor", got.Cmd)
	}
	// The subheader names the entry and the Config dashboard, which is where the
	// override that changes it is written.
	status := FormatAttendedAgentStatus(got)
	if status != "agent Cursor · "+ui.ConfigDashboardKeyLabel {
		t.Fatalf("status = %q", status)
	}
}

// The attended renders read the merged config, so the override layer the Config
// dashboard writes is what a gate menu, a pane title and the dashboard
// subheader report — this is the whole visibility discipline after ADR-0202
// decision 5 retired the session-lived picker.
func TestAttendedRendersResolveThroughTheOverrideLayer(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config", "config.toml")
	overridePath := filepath.Join(dataDir, "pop", "config.override.toml")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(userPath, `
[work.attended]
agents = [{ display_name = "Claude Usual", cmd = "claude --model opus" }]
`)
	write(overridePath, `
[work.attended]
agents = [{ display_name = "Cursor", cmd = "cursor" }]
`)

	cfgDeps := &config.Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
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
	}}
	cfg, err := config.LoadWith(cfgDeps, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}

	label := FormatAgentEntry(EffectiveAttendedEntry(cfg))
	if label != "Cursor" {
		t.Fatalf("entry render = %q, want the override's entry", label)
	}
	if !strings.Contains(FormatAttendedAgentStatus(EffectiveAttendedEntry(cfg)), "Cursor") {
		t.Fatalf("subheader = %q", FormatAttendedAgentStatus(EffectiveAttendedEntry(cfg)))
	}

	// The gate menu's Assists row is the same render, resolved from the same
	// merged config.
	spec := ui.GateMenuSpec{Items: []ui.GateMenuItem{
		{Key: "1", Label: "Get agent assistance (default)", Default: true, Assists: true},
		{Key: "0", Label: "Exit"},
	}}
	var got string
	orig := runGateMenu
	defer func() { runGateMenu = orig }()
	runGateMenu = func(spec ui.GateMenuSpec, _ io.Reader, _ io.Writer, _ ui.GateMenuRunConfig) (ui.GateMenuResult, error) {
		got = spec.AttendedLabel
		return ui.GateMenuResult{Key: "0"}, nil
	}
	in := strings.NewReader("")
	if _, _, err := promptGateMenu(&strings.Builder{}, in, newPromptReader(in), spec, nil, cfg); err != nil {
		t.Fatal(err)
	}
	if got != "Cursor" {
		t.Fatalf("gate AttendedLabel = %q, want Cursor", got)
	}
}
