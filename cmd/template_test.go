package cmd

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func TestTemplateCommandTree(t *testing.T) {
	tests := []struct {
		path    []string
		wantCmd any
		wantRun any
	}{
		{path: []string{"template", "list"}, wantCmd: templateListCmd, wantRun: runTemplateList},
		{path: []string{"template", "apply"}, wantCmd: templateApplyCmd, wantRun: runTemplateApply},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			got, _, err := rootCmd.Find(tt.path)
			if err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
			if got != tt.wantCmd {
				t.Fatalf("Find(%v) = %q, want template subcommand", tt.path, got.CommandPath())
			}
			if reflect.ValueOf(got.RunE).Pointer() != reflect.ValueOf(tt.wantRun).Pointer() {
				t.Fatalf("%q does not use the expected handler", got.CommandPath())
			}
		})
	}
}

func TestRunTemplateListWith(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{
			{Name: "dev"},
			{Name: "review"},
		},
	}
	var out bytes.Buffer

	if err := runTemplateListWith(cfg, &out); err != nil {
		t.Fatalf("runTemplateListWith() error: %v", err)
	}
	if got, want := out.String(), "dev\nreview\n"; got != want {
		t.Fatalf("list output = %q, want %q", got, want)
	}
}

func TestRunTemplateApplyWith(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "new-window":
				return "%42", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:   tmux,
		Getwd:  func() (string, error) { return "/repo/checkout", nil },
		ErrOut: io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{window_name}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session", "-n", "work", "-c", "/repo/checkout"},
		{"select-pane", "-t", "%42", "-T", "server"},
		{"send-keys", "-t", "%42", "go test ./...", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestRunTemplateApplyWithUnknownName(t *testing.T) {
	err := runTemplateApplyWith(templateRuntimeDeps{Tmux: &deps.MockTmux{}}, &config.Config{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `session template "missing" not found`) {
		t.Fatalf("error = %q, want clear unknown-template message", err.Error())
	}
}

func TestRunTemplateApplyWithTmuxFailure(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "display-message" {
				return "current-session", nil
			}
			if args[0] == "new-window" {
				return "", fmt.Errorf("tmux refused")
			}
			return "", nil
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo/checkout", nil },
	}

	err := runTemplateApplyWith(d, cfg, "dev")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `failed to create template window "work"`) {
		t.Fatalf("error = %q, want window creation context", err.Error())
	}
}

func TestRunTemplateApplyWithFlatWeightedSplits(t *testing.T) {
	// Test: window with 3 panes in a row with weights 1, 2, 3
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "weighted",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{
					Direction: "row",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left", Weight: 1},
						{Name: "middle", Command: "echo middle", Weight: 2},
						{Name: "right", Command: "echo right", Weight: 3},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				// Return incrementing pane IDs
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "weighted"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify split-window calls used -h flag (row = side-by-side)
	splitCount := 0
	for _, call := range calls {
		if call[0] == "split-window" {
			splitCount++
			if call[1] != "-h" {
				t.Errorf("split-window call %v should use -h for row direction", call)
			}
		}
	}
	if splitCount != 2 {
		t.Errorf("expected 2 split-window calls, got %d", splitCount)
	}

	// Verify resize-pane calls
	resizeCount := 0
	for _, call := range calls {
		if call[0] == "resize-pane" {
			resizeCount++
			// Should use -x flag for row direction
			if call[3] != "-x" {
				t.Errorf("resize-pane call %v should use -x for row direction", call)
			}
		}
	}
	if resizeCount != 3 {
		t.Errorf("expected 3 resize-pane calls, got %d", resizeCount)
	}
}

func TestRunTemplateApplyWithColumnDirection(t *testing.T) {
	// Test: column direction uses -v for splits and -y for resize
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "stacked",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{
					Direction: "column",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "top", Command: "echo top"},
						{Name: "bottom", Command: "echo bottom"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "stacked"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify split-window used -v flag (column = stacked)
	foundSplit := false
	for _, call := range calls {
		if call[0] == "split-window" {
			foundSplit = true
			if call[1] != "-v" {
				t.Errorf("split-window call %v should use -v for column direction", call)
			}
		}
	}
	if !foundSplit {
		t.Error("expected at least one split-window call")
	}

	// Verify resize-pane used -y flag for column direction
	foundResize := false
	for _, call := range calls {
		if call[0] == "resize-pane" {
			foundResize = true
			if call[3] != "-y" {
				t.Errorf("resize-pane call %v should use -y for column direction", call)
			}
		}
	}
	if !foundResize {
		t.Error("expected at least one resize-pane call")
	}
}

func TestRunTemplateApplyWithNestedContainers(t *testing.T) {
	// Test: nested containers - outer row with 2 children, first child is a column container
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "nested",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{
					Direction: "row",
					Panes: []config.SessionTemplatePaneSpec{
						{
							Direction: "column",
							Weight:    1,
							Panes: []config.SessionTemplatePaneSpec{
								{Name: "top-left", Command: "echo tl", Weight: 1},
								{Name: "bottom-left", Command: "echo bl", Weight: 1},
							},
						},
						{Name: "right", Command: "echo right", Weight: 1},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "nested"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Should have split-window calls for both outer and inner containers
	splitCount := 0
	for _, call := range calls {
		if call[0] == "split-window" {
			splitCount++
		}
	}
	// Outer container splits once (2 children), inner container splits once (2 children)
	if splitCount < 2 {
		t.Errorf("expected at least 2 split-window calls for nested containers, got %d", splitCount)
	}
}

func TestRunTemplateApplyWithDefaultWeight(t *testing.T) {
	// Test: omitted weight defaults to 1 (equal split)
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "equal",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{
					Direction: "row",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left"},  // weight omitted = 1
						{Name: "right", Command: "echo right"}, // weight omitted = 1
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "100", nil
					}
					if args[2] == "#{window_height}" {
						return "50", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "equal"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify resize-pane calls with equal sizes (50 each for 100 width)
	resizeCalls := []int{}
	for _, call := range calls {
		if call[0] == "resize-pane" && call[3] == "-x" {
			var size int
			fmt.Sscanf(call[4], "%d", &size)
			resizeCalls = append(resizeCalls, size)
		}
	}
	if len(resizeCalls) != 2 {
		t.Fatalf("expected 2 resize-pane calls, got %d", len(resizeCalls))
	}
	// Both should be 50 (equal split of 100)
	if resizeCalls[0] != 50 || resizeCalls[1] != 50 {
		t.Errorf("expected equal sizes [50, 50], got %v", resizeCalls)
	}
}

func TestRunTemplateApplyWithDeepNesting(t *testing.T) {
	// Test: 3 levels deep nesting
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "deep",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Pane: &config.SessionTemplatePaneSpec{
					Direction: "row",
					Panes: []config.SessionTemplatePaneSpec{
						{
							Direction: "column",
							Panes: []config.SessionTemplatePaneSpec{
								{
									Direction: "row",
									Panes: []config.SessionTemplatePaneSpec{
										{Name: "deep-left", Command: "echo dl"},
										{Name: "deep-right", Command: "echo dr"},
									},
								},
								{Name: "bottom", Command: "echo bottom"},
							},
						},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "deep"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Should successfully create all panes at all nesting levels
	// Count send-keys calls (one per leaf pane)
	sendKeysCount := 0
	for _, call := range calls {
		if call[0] == "send-keys" {
			sendKeysCount++
		}
	}
	// 4 leaf panes: deep-left, deep-right, bottom, right
	if sendKeysCount != 4 {
		t.Errorf("expected 4 send-keys calls for 4 leaf panes, got %d", sendKeysCount)
	}
}

func TestRunTemplateApplyWithMultipleWindows(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{
				{Name: "work", Pane: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."}},
				{Name: "logs", Pane: &config.SessionTemplatePaneSpec{Name: "tail", Command: "tail -f app.log"}},
			},
		}},
	}
	var calls [][]string
	newWindowCount := 0
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				id := fmt.Sprintf("%%%d", newWindowCount)
				newWindowCount++
				return id, nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:   tmux,
		Getwd:  func() (string, error) { return "/repo/checkout", nil },
		ErrOut: io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{window_name}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session", "-n", "work", "-c", "/repo/checkout"},
		{"select-pane", "-t", "%0", "-T", "server"},
		{"send-keys", "-t", "%0", "go test ./...", "Enter"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session", "-n", "logs", "-c", "/repo/checkout"},
		{"select-pane", "-t", "%1", "-T", "tail"},
		{"send-keys", "-t", "%1", "tail -f app.log", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestRunTemplateApplyWithSkipExistingWindow(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{
				{Name: "work", Pane: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."}},
				{Name: "logs", Pane: &config.SessionTemplatePaneSpec{Name: "tail", Command: "tail -f app.log"}},
			},
		}},
	}
	var calls [][]string
	var warnings bytes.Buffer
	newWindowCount := 0
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "work\n", nil
			case "new-window":
				id := fmt.Sprintf("%%%d", newWindowCount)
				newWindowCount++
				return id, nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:   tmux,
		Getwd:  func() (string, error) { return "/repo/checkout", nil },
		ErrOut: &warnings,
	}

	if err := runTemplateApplyWith(d, cfg, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Only the "logs" window should be created; "work" is skipped.
	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{window_name}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session", "-n", "logs", "-c", "/repo/checkout"},
		{"select-pane", "-t", "%0", "-T", "tail"},
		{"send-keys", "-t", "%0", "tail -f app.log", "Enter"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
	warnStr := warnings.String()
	if !strings.Contains(warnStr, "work") || !strings.Contains(warnStr, "skipping") {
		t.Fatalf("expected skip warning for existing window, got %q", warnStr)
	}
}
