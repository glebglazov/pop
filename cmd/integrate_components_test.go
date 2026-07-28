package cmd

import (
	"strings"
	"testing"
)

// TestPositiveIntegrateFlagsHardError: --pane-skill and --task-skills are rejected.
func TestPositiveIntegrateFlagsHardError(t *testing.T) {
	t.Parallel()
	prevPane := integratePaneSkill
	prevTask := integrateTaskSkills
	prevUpdate := integrateUpdateExisting
	t.Cleanup(func() {
		integratePaneSkill = prevPane
		integrateTaskSkills = prevTask
		integrateUpdateExisting = prevUpdate
	})
	integrateUpdateExisting = false

	integratePaneSkill = true
	integrateTaskSkills = false
	if err := runIntegrate(integrateCmd, []string{"claude"}); err == nil {
		t.Fatal("expected error for --pane-skill")
	} else if !strings.Contains(err.Error(), "--pane-skill") || !strings.Contains(err.Error(), "integrations") {
		t.Fatalf("unexpected error: %v", err)
	}

	integratePaneSkill = false
	integrateTaskSkills = true
	if err := runIntegrate(integrateCmd, []string{"claude"}); err == nil {
		t.Fatal("expected error for --task-skills")
	} else if !strings.Contains(err.Error(), "--task-skills") || !strings.Contains(err.Error(), "integrations") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIntegrateCmd_UpdateExistingWithAgentArgIsError(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = true
	t.Cleanup(func() { integrateUpdateExisting = prev })

	err := integrateCmd.Args(integrateCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error when --update-existing is combined with an agent argument")
	}
	if !strings.Contains(err.Error(), "--update-existing") {
		t.Errorf("error should mention --update-existing, got %q", err.Error())
	}
}

func TestIntegrateCmd_UpdateExistingWithNoArgsIsOK(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = true
	t.Cleanup(func() { integrateUpdateExisting = prev })

	if err := integrateCmd.Args(integrateCmd, []string{}); err != nil {
		t.Errorf("expected no error for --update-existing with no args, got %v", err)
	}
}

func TestIntegrateCmd_WithoutFlagRequiresAtLeastOneArg(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = false
	t.Cleanup(func() { integrateUpdateExisting = prev })

	if err := integrateCmd.Args(integrateCmd, []string{}); err == nil {
		t.Error("expected error when no agent argument is provided")
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude"}); err != nil {
		t.Errorf("expected no error for single agent arg, got %v", err)
	}
}

func TestIntegrateCmd_OverwriteConflictsWithUpdateExistingIsError(t *testing.T) {
	prevUpdate := integrateUpdateExisting
	prevOverwrite := integrateOverwriteConflicts
	integrateUpdateExisting = true
	integrateOverwriteConflicts = true
	t.Cleanup(func() {
		integrateUpdateExisting = prevUpdate
		integrateOverwriteConflicts = prevOverwrite
	})

	err := runIntegrate(integrateCmd, nil)
	if err == nil {
		t.Fatal("expected error when --overwrite-conflicts is combined with --update-existing")
	}
	if !strings.Contains(err.Error(), "--overwrite-conflicts") || !strings.Contains(err.Error(), "--update-existing") {
		t.Errorf("error should mention both flags, got %q", err.Error())
	}
}
