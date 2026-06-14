package cmd

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDashboardCommandTree(t *testing.T) {
	tests := []struct {
		path    []string
		wantCmd *cobra.Command
		wantRun func(*cobra.Command, []string) error
	}{
		{path: []string{"monitor", "dashboard"}, wantCmd: monitorDashboardCmd, wantRun: runDashboard},
		{path: []string{"project", "dashboard"}, wantCmd: projectDashboardCmd, wantRun: runProject},
		{path: []string{"worktree", "dashboard"}, wantCmd: worktreeDashboardCmd, wantRun: runWorktree},
		{path: []string{"queue", "integrate"}, wantCmd: queueIntegrateCmd, wantRun: runQueueIntegrate},
		{path: []string{"queue", "abandon"}, wantCmd: queueAbandonCmd, wantRun: runQueueAbandon},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			got, _, err := rootCmd.Find(tt.path)
			if err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
			if got != tt.wantCmd {
				t.Fatalf("Find(%v) = %q, want %q", tt.path, got.CommandPath(), tt.wantCmd.CommandPath())
			}
			if reflect.ValueOf(got.RunE).Pointer() != reflect.ValueOf(tt.wantRun).Pointer() {
				t.Fatalf("%q does not use the expected picker handler", got.CommandPath())
			}
		})
	}
}

func TestLegacyDashboardAliasIsHidden(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if got != dashboardCmd {
		t.Fatalf("Find([dashboard]) = %q, want legacy dashboard alias", got.CommandPath())
	}
	if !dashboardCmd.Hidden {
		t.Fatal("legacy dashboard alias must stay hidden")
	}
	if reflect.ValueOf(dashboardCmd.RunE).Pointer() != reflect.ValueOf(runDashboard).Pointer() {
		t.Fatal("legacy dashboard alias does not use the dashboard handler")
	}

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := rootCmd.Help(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	if strings.Contains(help, "\n  dashboard ") {
		t.Fatalf("root help exposes legacy dashboard alias:\n%s", help)
	}
	for _, command := range []string{"monitor", "project", "worktree"} {
		if !strings.Contains(help, "\n  "+command+" ") {
			t.Fatalf("root help missing %q namespace:\n%s", command, help)
		}
	}
}

func TestLegacyPickerCompatibilityPaths(t *testing.T) {
	tests := []struct {
		cmd     *cobra.Command
		wantRun func(*cobra.Command, []string) error
	}{
		{cmd: projectCmd, wantRun: runProject},
		{cmd: worktreeCmd, wantRun: runWorktree},
	}

	for _, tt := range tests {
		t.Run(tt.cmd.Name(), func(t *testing.T) {
			if reflect.ValueOf(tt.cmd.RunE).Pointer() != reflect.ValueOf(tt.wantRun).Pointer() {
				t.Fatalf("%q no longer supports the legacy direct picker path", tt.cmd.CommandPath())
			}
		})
	}
}
