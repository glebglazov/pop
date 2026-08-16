package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// autoDrainBit reads a set's consent bit through its own writer: setting it to
// the value it already holds reports no change.
func autoDrainBit(t *testing.T, td *tasks.Deps, defPath, setID string) bool {
	t.Helper()
	changed, err := tasks.SetTaskSetAutoDrain(td, defPath, setID, false)
	if err != nil {
		t.Fatalf("read auto-drain bit for %s: %v", setID, err)
	}
	if !changed {
		return false
	}
	// Clearing it changed something, so it was on — put it back.
	if _, err := tasks.SetTaskSetAutoDrain(td, defPath, setID, true); err != nil {
		t.Fatalf("restore auto-drain bit for %s: %v", setID, err)
	}
	return true
}

// TestTaskRegisterAutoDrainOnNamedAlreadyRegisteredSet pins the fix for a silent
// no-op: naming a set that is already registered and passing --auto-drain used to
// drop the flag entirely, because consent was written only for first-time
// registrations. The human's `pop tasks register <set> --auto-drain` then
// returned READY with the bit still off and the daemon never picked the set up.
// Consent is not a binding, so the never-rebind rule does not reach it.
func TestTaskRegisterAutoDrainOnNamedAlreadyRegisteredSet(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubTaskConfigProjects(t, root)
	defPath := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, defPath, "draft")

	var first bytes.Buffer
	if err := runTaskRegisterWith(td, &first, ""); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if autoDrainBit(t, td, defPath, "draft") {
		t.Fatal("a register without --auto-drain must leave the consent bit off")
	}

	taskRegisterAutoDrain = true
	var second bytes.Buffer
	if err := runTaskRegisterWith(td, &second, "draft"); err != nil {
		t.Fatalf("re-register --auto-drain: %v", err)
	}
	if !autoDrainBit(t, td, defPath, "draft") {
		t.Fatalf("re-registering a named set with --auto-drain left the bit off:\n%s", second.String())
	}
	// The set is bound to the Trunk worktree and now consents, which is exactly
	// the shape ADR-0192 wants said out loud — raising the bit is what provokes
	// it, not the bind that happened one register earlier.
	if warnings := trunkAutoDrainWarnings(second.String()); len(warnings) != 1 {
		t.Fatalf("want one trunk auto-drain warning, got %d:\n%s", len(warnings), second.String())
	}
}

// TestTaskRegisterAutoDrainDoesNotWidenToTheBacklog is the other half of the
// rule: without a set argument the flag still reaches only the registrations this
// invocation created, so a bare `pop tasks register --auto-drain` in a repo full
// of registered sets cannot enroll them all in unattended draining.
func TestTaskRegisterAutoDrainDoesNotWidenToTheBacklog(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubTaskConfigProjects(t, root)
	defPath := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, defPath, "older")

	var first bytes.Buffer
	if err := runTaskRegisterWith(td, &first, ""); err != nil {
		t.Fatalf("first register: %v", err)
	}

	// A second set appears, and the bare register that activates it consents on
	// its behalf alone.
	writeTaskThoughts(t, defPath, "newer")
	taskRegisterAutoDrain = true
	var second bytes.Buffer
	if err := runTaskRegisterWith(td, &second, ""); err != nil {
		t.Fatalf("second register: %v", err)
	}

	if !autoDrainBit(t, td, defPath, "newer") {
		t.Fatalf("the newly registered set must consent:\n%s", second.String())
	}
	if autoDrainBit(t, td, defPath, "older") {
		t.Fatalf("a bare register must not raise consent for an already-registered set:\n%s", second.String())
	}
	if warnings := trunkAutoDrainWarnings(second.String()); len(warnings) != 1 || !strings.Contains(warnings[0], "newer") {
		t.Fatalf("want one trunk warning naming the new set, got:\n%s", strings.Join(warnings, "\n"))
	}
}
