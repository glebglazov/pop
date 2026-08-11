package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// storedOverride returns the override file's text, or "" when pop stores
// nothing — the two states a refusal has to keep apart from a write.
func storedOverride(t *testing.T, f *overrideFixture) string {
	t.Helper()
	data, err := os.ReadFile(f.overridePath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read override file: %v", err)
	}
	return string(data)
}

// TestStoreOverrideBufferReachesTheMergedConfig is the whole point of the
// editor: what a human types into $EDITOR is what every later config load
// resolves for that key.
func TestStoreOverrideBufferReachesTheMergedConfig(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)

	problem, err := StoreOverrideBufferWith(f.d, "work.verify.agents",
		`work.verify.agents = ["codex --model gpt", "claude"]`)
	if err != nil || problem != "" {
		t.Fatalf("StoreOverrideBufferWith() = %q, %v; want a clean write", problem, err)
	}

	cfg := f.load(t)
	if got := cfg.VerifyAgents(); !reflect.DeepEqual(got, []string{"codex --model gpt", "claude"}) {
		t.Fatalf("VerifyAgents() = %#v, want the edited list", got)
	}
	if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Errorf("ImplementAgents() = %#v, want the hand-authored list untouched", got)
	}
}

// TestStoreOverrideBufferAcceptsAnExplicitlyEmptyList is the half of ADR-0202
// decision 6 the editor owns: emptiness stated on purpose is a value, and the
// preview then reports the fallthrough as disabled.
func TestStoreOverrideBufferAcceptsAnExplicitlyEmptyList(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)

	problem, err := StoreOverrideBufferWith(f.d, "work.routine.agents", "work.routine.agents = []\n")
	if err != nil || problem != "" {
		t.Fatalf("StoreOverrideBufferWith() = %q, %v; want a clean write", problem, err)
	}

	view := viewFor(t, f, "work.routine.agents")
	if !view.Overridden {
		t.Fatal("Overridden = false after storing an empty list")
	}
	if view.Note != overrideNoteEmptyOverride {
		t.Errorf("Note = %q, want the disabled fallthrough said in words", view.Note)
	}
}

// TestStoreOverrideBufferRefusesRatherThanWriting walks every way a buffer can
// come back wrong. Each one is a problem the human is sent back to fix, and in
// every case the override file is left exactly as it was: a file pop wrote
// itself must never be the source of a finding (ADR-0202 decision 8).
func TestStoreOverrideBufferRefusesRatherThanWriting(t *testing.T) {
	cases := []struct {
		name   string
		buffer string
		says   string
	}{
		{
			name:   "not TOML at all",
			buffer: "work.verify.agents = [claude",
			says:   "not valid TOML",
		},
		{
			name:   "a type the key cannot hold",
			buffer: `work.verify.agents = "claude"`,
			says:   "cannot hold this value",
		},
		{
			name:   "a value that would be a config finding",
			buffer: `work.verify.agents = [{ display_name = "Claude" }]`,
			says:   "is malformed",
		},
		{
			name:   "an entry that names no command",
			buffer: `work.verify.agents = [""]`,
			says:   "is malformed",
		},
		{
			name:   "some other key entirely",
			buffer: `work.implement.agents = ["claude"]`,
			says:   "has to set work.verify.agents",
		},
		{
			name:   "the right key and a second one",
			buffer: "work.verify.agents = [\"claude\"]\nworktree.root = \"/tmp\"\n",
			says:   "One buffer overrides one key",
		},
		{
			name:   "nothing at all",
			buffer: "  \n\t\n",
			says:   "buffer is empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOverrideFixture(t)
			writeConfigFile(t, f.userPath, handAuthoredWork)

			problem, err := StoreOverrideBufferWith(f.d, "work.verify.agents", tc.buffer)
			if err != nil {
				t.Fatalf("StoreOverrideBufferWith() error: %v; want a problem, not a failure", err)
			}
			if !strings.Contains(problem, tc.says) {
				t.Errorf("problem = %q, want it to say %q", problem, tc.says)
			}
			if stored := storedOverride(t, f); stored != "" {
				t.Errorf("override file written despite the refusal:\n%s", stored)
			}
		})
	}
}

// TestStoreOverrideBufferIgnoresPopsOwnNotes keeps the re-open loop honest: the
// comment lines pop puts above a refused value travel back with the buffer and
// must not change what is stored.
func TestStoreOverrideBufferIgnoresPopsOwnNotes(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)

	buffer := "# pop: this value was not stored.\n# pop: Loading this value would report: something\n" +
		`work.verify.agents = ["codex"]` + "\n"
	problem, err := StoreOverrideBufferWith(f.d, "work.verify.agents", buffer)
	if err != nil || problem != "" {
		t.Fatalf("StoreOverrideBufferWith() = %q, %v; want a clean write", problem, err)
	}
	if got := f.load(t).VerifyAgents(); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("VerifyAgents() = %#v, want the value under the notes", got)
	}
}

// TestStoreOverrideBufferKeepsAgentTables covers the richer entry shape: a
// {display_name, cmd} table has to survive the round trip through the file and
// come back out as an entry, not as text.
func TestStoreOverrideBufferKeepsAgentTables(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)

	problem, err := StoreOverrideBufferWith(f.d, "work.attended.agents",
		`work.attended.agents = ["claude", { display_name = "Codex", cmd = "codex --model gpt" }]`)
	if err != nil || problem != "" {
		t.Fatalf("StoreOverrideBufferWith() = %q, %v; want a clean write", problem, err)
	}

	cfg := f.load(t)
	entries := cfg.Work.Attended.Agents
	if len(entries) != 2 || entries[1].DisplayName != "Codex" || entries[1].Cmd != "codex --model gpt" {
		t.Fatalf("attended agents = %#v, want the table entry intact", entries)
	}
	view := viewFor(t, f, "work.attended.agents")
	if !strings.Contains(view.EffectiveTOML, `display_name = "Codex"`) {
		t.Errorf("EffectiveTOML = %q, want the table rendered as config", view.EffectiveTOML)
	}
}

// TestCopyOverrideFromSource is the reversibility ADR-0202 decision 6 trades a
// confirmation prompt for: the source value is always one keystroke away.
func TestCopyOverrideFromSource(t *testing.T) {
	t.Run("copies the hand-authored value down", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)

		if err := CopyOverrideFromSourceWith(f.d, f.userPath, "work.implement.agents"); err != nil {
			t.Fatalf("CopyOverrideFromSourceWith() error: %v", err)
		}
		view := viewFor(t, f, "work.implement.agents")
		if !view.Overridden || view.Provenance() != "override" {
			t.Fatalf("view = %+v, want the copy in force as an override", view)
		}
		if !strings.Contains(view.EffectiveTOML, `"claude"`) {
			t.Errorf("EffectiveTOML = %q, want the hand-authored value copied down", view.EffectiveTOML)
		}
		if got := view.SourceTOML; !strings.Contains(got, `"claude"`) {
			t.Errorf("SourceTOML = %q, want the hand-authored value still below it", got)
		}
	})

	t.Run("replaces an override that is already there", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)
		if err := SetOverrideValueWith(f.d, "work.implement.agents", []any{"codex"}); err != nil {
			t.Fatalf("SetOverrideValueWith() error: %v", err)
		}

		if err := CopyOverrideFromSourceWith(f.d, f.userPath, "work.implement.agents"); err != nil {
			t.Fatalf("CopyOverrideFromSourceWith() error: %v", err)
		}
		if got := f.load(t).ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
			t.Fatalf("ImplementAgents() = %#v, want the source value back", got)
		}
	})

	t.Run("copies the empty value when no layer defines the key", func(t *testing.T) {
		f := newOverrideFixture(t)

		if err := CopyOverrideFromSourceWith(f.d, f.userPath, "work.verify.agents"); err != nil {
			t.Fatalf("CopyOverrideFromSourceWith() error: %v", err)
		}
		view := viewFor(t, f, "work.verify.agents")
		if !view.Overridden {
			t.Fatal("Overridden = false after copying the source down")
		}
		if view.Note != overrideNoteEmptyOverride {
			t.Errorf("Note = %q; copying an empty source down is still a real empty value", view.Note)
		}
	})

	t.Run("refuses a key that is not exposed", func(t *testing.T) {
		f := newOverrideFixture(t)
		if err := CopyOverrideFromSourceWith(f.d, f.userPath, "worktree.root"); err == nil {
			t.Fatal("no error copying a key no human can override here")
		}
	})
}

// TestDeleteOverrideRestoresTheSource is the remove action's contract, read
// through the merged config rather than the file.
func TestDeleteOverrideRestoresTheSource(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)
	if err := SetOverrideValueWith(f.d, "work.implement.agents", []any{"codex"}); err != nil {
		t.Fatalf("SetOverrideValueWith() error: %v", err)
	}

	if err := DeleteOverrideValueWith(f.d, "work.implement.agents"); err != nil {
		t.Fatalf("DeleteOverrideValueWith() error: %v", err)
	}
	if got := f.load(t).ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("ImplementAgents() = %#v, want the hand-authored list restored", got)
	}
	// Removing the last override takes the file with it, so a second remove has
	// nothing to do and says nothing about it.
	if err := DeleteOverrideValueWith(f.d, "work.implement.agents"); err != nil {
		t.Fatalf("second DeleteOverrideValueWith() error: %v", err)
	}
	if stored := storedOverride(t, f); stored != "" {
		t.Errorf("override file left behind:\n%s", stored)
	}
}
