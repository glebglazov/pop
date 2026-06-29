package cmd

import (
	"bytes"
	"fmt"
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
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo/checkout", nil },
	}

	if err := runTemplateApplyWith(d, cfg, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "#S"},
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
