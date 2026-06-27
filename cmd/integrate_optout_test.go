package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// This file covers slice 08: persisted per-agent Component opt-out (negative
// consent) and the reconciling refresh that adds missing defaults while
// respecting it (ADR 0064).

// logsContain reports whether any captured log line contains sub.
func logsContain(logs []string, sub string) bool {
	for _, l := range logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// capturingFactories returns dry/real deps factories sharing one fake FS, each
// wired to append to *logs so a test can assert which refresh paths fired.
func capturingFactories(home string, fs *fakeFS, logs *[]string) (dry, real func() *integrateDeps) {
	logf := func(format string, args ...any) { *logs = append(*logs, fmt.Sprintf(format, args...)) }
	dry = func() *integrateDeps {
		d := withDryRun(fakeDeps(home, fs, io.Discard))
		d.logf = logf
		return d
	}
	real = func() *integrateDeps {
		d := fakeDeps(home, fs, io.Discard)
		d.logf = logf
		return d
	}
	return dry, real
}

func claudeUpdated(updated []string) bool {
	for _, a := range updated {
		if a == "claude" {
			return true
		}
	}
	return false
}

// ----- opt-out persistence ---------------------------------------------------

// TestInstallNoFlagPersistsOptOut: declining a component with `--no-*` at
// install time records a per-agent opt-out; the components actually installed
// are not opted out.
func TestInstallNoFlagPersistsOptOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateInstall(d, "claude", false, false, true /*noPaneSkill*/, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}

	set := fs.loadOptOut("claude")
	if !set[ComponentPaneSkill] {
		t.Fatalf("expected pane-skill opt-out persisted, got %v", set)
	}
	if set[ComponentTaskSkills] {
		t.Fatalf("task-skills was installed and must not be opted out, got %v", set)
	}
}

// TestRemovePersistsOptOut: removing a component via `pop integrate remove`
// records a per-agent opt-out for that component only.
func TestRemovePersistsOptOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := installComponentSet(d, "claude",
		[]ComponentID{ComponentPaneSkill, ComponentTaskSkills}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := runIntegrateRemoveComponents(d, "claude", []ComponentID{ComponentPaneSkill}); err != nil {
		t.Fatalf("remove pane-skill: %v", err)
	}

	set := fs.loadOptOut("claude")
	if !set[ComponentPaneSkill] {
		t.Fatalf("expected pane-skill opt-out after remove, got %v", set)
	}
	if set[ComponentTaskSkills] {
		t.Fatalf("task-skills was not removed and must not be opted out, got %v", set)
	}
}

// TestRemoveMergesIntoExistingOptOut: a targeted remove adds to (does not
// replace) an agent's existing opt-out set.
func TestRemoveMergesIntoExistingOptOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	// Seed an existing opt-out for the pane skill.
	fs.optOuts["claude"] = map[ComponentID]bool{ComponentPaneSkill: true}

	if err := installComponentSet(d, "claude", []ComponentID{ComponentTaskSkills}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := runIntegrateRemoveComponents(d, "claude", []ComponentID{ComponentTaskSkills}); err != nil {
		t.Fatalf("remove task-skills: %v", err)
	}

	set := fs.loadOptOut("claude")
	if !set[ComponentPaneSkill] || !set[ComponentTaskSkills] {
		t.Fatalf("remove must merge into the existing opt-out set, got %v", set)
	}
}

// ----- bare integrate clears opt-outs ---------------------------------------

// TestBareIntegrateClearsOptOutsAndInstallsFullSet: a bare `pop integrate
// <agent>` re-asserts the full default set and clears any prior opt-outs.
func TestBareIntegrateClearsOptOutsAndInstallsFullSet(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, io.Discard)

	// Pre-existing opt-outs for both default components.
	fs.optOuts["claude"] = map[ComponentID]bool{ComponentPaneSkill: true, ComponentTaskSkills: true}

	if err := runIntegrateComponents(d, "claude", nil); err != nil {
		t.Fatalf("bare integrate: %v", err)
	}

	if set := fs.loadOptOut("claude"); len(set) != 0 {
		t.Fatalf("bare integrate must clear opt-outs, got %v", set)
	}
	_, paneDest, paneTarget := paneSkillPaths()
	if fs.symlinks[paneDest] != paneTarget {
		t.Fatalf("pane skill not installed by bare integrate: %q -> %q", paneDest, fs.symlinks[paneDest])
	}
	if _, ok := fs.symlinks[grillSkillDest()]; !ok {
		t.Fatalf("task planning skills not installed by bare integrate")
	}
}

// TestInstallNoFlagReplacesPriorOptOut: an install that declines one component
// rewrites the agent's opt-out set to exactly that component — a previously
// opted-out, now-installed component is no longer opted out.
func TestInstallNoFlagReplacesPriorOptOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, io.Discard)

	// Previously both were declined.
	fs.optOuts["claude"] = map[ComponentID]bool{ComponentPaneSkill: true, ComponentTaskSkills: true}

	// Now decline only the pane skill (task skills install again).
	if err := runIntegrateInstall(d, "claude", false, false, true /*noPaneSkill*/, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}

	set := fs.loadOptOut("claude")
	if !set[ComponentPaneSkill] {
		t.Fatalf("pane-skill should remain opted out, got %v", set)
	}
	if set[ComponentTaskSkills] {
		t.Fatalf("task-skills was reinstalled and must no longer be opted out, got %v", set)
	}
}

// ----- refresh adds missing defaults ----------------------------------------

// TestRefreshAddsMissingDefaultOnIntegratedAgent: an already-integrated agent
// (status wiring present) missing a non-opted-out default component picks it up
// on refresh, without any prompt.
func TestRefreshAddsMissingDefaultOnIntegratedAgent(t *testing.T) {
	fs := newFakeFS()
	// Status wiring only → integrated, but the skills are missing.
	installViaFake(t, fs, installerHome, "claude")

	_, paneDest, paneTarget := paneSkillPaths()
	if _, ok := fs.symlinks[paneDest]; ok {
		t.Fatalf("precondition: pane skill must not be installed yet")
	}

	result := updateStaleIntegrations(fakeFactories(installerHome, fs))
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
	if fs.symlinks[paneDest] != paneTarget {
		t.Fatalf("refresh did not add the missing pane skill: %q -> %q", paneDest, fs.symlinks[paneDest])
	}
	if _, ok := fs.symlinks[grillSkillDest()]; !ok {
		t.Fatalf("refresh did not add the missing task planning skills (symlinks=%v)", fs.symlinks)
	}
	if !claudeUpdated(result.Updated) {
		t.Fatalf("claude should be reported updated after adding missing defaults, got %v", result.Updated)
	}
}

// TestRefreshAddMissingLogsNewPath: the add-missing refresh path logs per slice
// 01.
func TestRefreshAddMissingLogsNewPath(t *testing.T) {
	fs := newFakeFS()
	installViaFake(t, fs, installerHome, "claude")

	var logs []string
	dry, real := capturingFactories(installerHome, fs, &logs)
	updateStaleIntegrations(dry, real)

	if !logsContain(logs, "missing default") {
		t.Fatalf("expected an add-missing log line, got %v", logs)
	}
	if !logsContain(logs, "added") {
		t.Fatalf("expected an 'added' log line, got %v", logs)
	}
}

// ----- refresh respects opt-out ---------------------------------------------

// TestRefreshNeverReAddsOptedOut: an integrated agent whose default skills are
// both opted out gets neither re-added by refresh (covers both groups).
func TestRefreshNeverReAddsOptedOut(t *testing.T) {
	fs := newFakeFS()
	installViaFake(t, fs, installerHome, "claude")
	fs.optOuts["claude"] = map[ComponentID]bool{ComponentPaneSkill: true, ComponentTaskSkills: true}

	result := updateStaleIntegrations(fakeFactories(installerHome, fs))

	if len(fs.symlinks) != 0 {
		t.Fatalf("opted-out components must not be re-added, got symlinks %v", fs.symlinks)
	}
	if claudeUpdated(result.Updated) {
		t.Fatalf("claude must not be reported updated when only opted-out skills are missing, got %v", result.Updated)
	}
}

// TestRefreshNeverUpdatesOptedOut: an opted-out component that is somehow still
// installed and stale is never updated by refresh — opt-out wins over reconcile.
// Both default skills are opted out so the not-installed one is not auto-added
// either, isolating the "never update" guarantee for the installed one.
func TestRefreshNeverUpdatesOptedOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, io.Discard)

	// Install status wiring + pane skill, then declare both default skills
	// opted out and corrupt the pane render so it reads as stale.
	if err := installComponentSet(d, "claude", []ComponentID{ComponentPaneSkill}); err != nil {
		t.Fatalf("install: %v", err)
	}
	fs.optOuts["claude"] = map[ComponentID]bool{ComponentPaneSkill: true, ComponentTaskSkills: true}

	renderFile := claudePaneRenderFile(installerHome)
	stale := []byte("stale skill body")
	if _, ok := fs.files[renderFile]; !ok {
		t.Fatalf("precondition: pane render file %s missing", renderFile)
	}
	fs.files[renderFile] = append([]byte{}, stale...)

	result := updateStaleIntegrations(fakeFactories(installerHome, fs))

	if !bytes.Equal(fs.files[renderFile], stale) {
		t.Fatalf("opted-out installed component must not be updated; render was refreshed to %q", fs.files[renderFile])
	}
	if claudeUpdated(result.Updated) {
		t.Fatalf("claude must not be reported updated for an opted-out component, got %v", result.Updated)
	}
}

// ----- agent with no integration is left alone ------------------------------

// TestRefreshLeavesUnintegratedAgentAlone: an agent with no pop integration at
// all is untouched by refresh — no defaults are added.
func TestRefreshLeavesUnintegratedAgentAlone(t *testing.T) {
	fs := newFakeFS() // nothing installed for any agent

	result := updateStaleIntegrations(fakeFactories(installerHome, fs))

	if len(fs.symlinks) != 0 || len(fs.files) != 0 {
		t.Fatalf("refresh must not touch an unintegrated agent: files=%v symlinks=%v",
			sortedKeys(fs.files), fs.symlinks)
	}
	if len(result.Updated) != 0 {
		t.Fatalf("no agent should be reported updated, got %v", result.Updated)
	}
}

// ----- opt-out persistence logs ---------------------------------------------

// TestPersistInstallOptOutLogs: persisting an install opt-out logs per slice 01.
func TestPersistInstallOptOutLogs(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	var logs []string
	d.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	if err := runIntegrateInstall(d, "claude", false, false, true /*noPaneSkill*/, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}
	if !logsContain(logs, "persistInstallOptOut") {
		t.Fatalf("expected a persistInstallOptOut log line, got %v", logs)
	}
}

// ----- state.json round-trip for opt-outs -----------------------------------

// TestAgentOptOut_StateRoundTrip: the production state.json-backed opt-out
// helpers persist and clear a per-agent set.
func TestAgentOptOut_StateRoundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := saveAgentOptOut("claude", map[ComponentID]bool{ComponentPaneSkill: true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadAgentOptOut("claude")
	if !got[ComponentPaneSkill] || got[ComponentTaskSkills] {
		t.Fatalf("round-trip mismatch, got %v", got)
	}

	// Clearing removes the entry.
	if err := saveAgentOptOut("claude", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := loadAgentOptOut("claude"); len(got) != 0 {
		t.Fatalf("clear should empty the set, got %v", got)
	}
}

// TestAgentOptOut_StatePreservesRevision: writing an opt-out does not disturb
// the build revision marker stored in the same state.json.
func TestAgentOptOut_StatePreservesRevision(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	seedState(t, "abc123")

	if err := saveAgentOptOut("claude", map[ComponentID]bool{ComponentTaskSkills: true}); err != nil {
		t.Fatalf("save opt-out: %v", err)
	}
	if got := readStateRevision(t); got != "abc123" {
		t.Fatalf("opt-out write clobbered build revision: got %q", got)
	}
	if !loadAgentOptOut("claude")[ComponentTaskSkills] {
		t.Fatalf("opt-out not persisted alongside revision")
	}
}
