package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The picker's half of the host contract (ADR-0202 decision 11): while the
// Config dashboard is open the picker's keys are suspended — ctrl+x above all,
// which the picker binds to force-deleting a worktree and the component binds to
// removing an override — and nothing on either side prints, because the picker's
// result is its stdout.

// pickerItems are two worktrees, so a suspended delete key has something real to
// have destroyed.
func pickerItems() []Item {
	return []Item{
		{Name: "main", Path: "/repo"},
		{Name: "feature", Path: "/repo-feature"},
	}
}

// worktreePickerOpts and projectPickerOpts are the two hosts as cmd builds them,
// minus what needs a repo: the key sets are what matters here.
func worktreePickerOpts() []PickerOption {
	return []PickerOption{WithDelete(), WithContext(), WithKillSession(), WithReset(), WithCreateWorktree()}
}

func projectPickerOpts() []PickerOption {
	return []PickerOption{WithKillSession(), WithReset()}
}

// pickerHosting builds a picker that hosts the component over a fake layer. The
// editor is scripted, so the edit path never reaches a real $EDITOR.
func pickerHosting(t *testing.T, editor *scriptedEditor, opts ...PickerOption) (*Picker, *fakeOverrideWriter) {
	t.Helper()
	writer := newFakeOverrideWriter()
	open := func() *ConfigDashboard {
		rows, err := writer.Rows()
		if err != nil {
			t.Fatalf("Rows() error: %v", err)
		}
		o := ConfigDashboardOpts{Writer: writer}
		if editor != nil {
			o.Editor = editor.open
		}
		return NewConfigDashboard(rows, o)
	}
	return sizedPicker(append(opts, WithConfigDashboard(open))...), writer
}

func sizedPicker(opts ...PickerOption) *Picker {
	p := NewPicker(pickerItems(), opts...)
	p.Init()
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return p
}

// drivePicker sends one message and runs the commands it produces to
// exhaustion, the way the tea runtime would, reporting whether the picker asked
// the program to quit.
func drivePicker(p *Picker, msg tea.Msg) bool {
	_, cmd := p.Update(msg)
	for cmd != nil {
		next := cmd()
		if next == nil {
			return false
		}
		if _, quit := next.(tea.QuitMsg); quit {
			return true
		}
		_, cmd = p.Update(next)
	}
	return false
}

func typeIntoPicker(p *Picker, text string) {
	for _, r := range text {
		drivePicker(p, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func altKey(r rune) tea.KeyPressMsg  { return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt} }
func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

var escKey = tea.KeyPressMsg{Code: tea.KeyEscape}
var enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}

func TestPickerConfigDashboardOpensOnTheGlobalChord(t *testing.T) {
	for name, opts := range map[string][]PickerOption{
		"worktree picker": worktreePickerOpts(),
		"project picker":  projectPickerOpts(),
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := pickerHosting(t, &scriptedEditor{}, opts...)

			if quit := drivePicker(p, altKey('c')); quit {
				t.Fatal("the chord quit the picker")
			}
			if !p.ConfigModalOpen() {
				t.Fatal("alt+c opened no Config dashboard")
			}
			if view := p.View().Content; !strings.Contains(view, "Config · keys you can override") {
				t.Fatalf("view over the picker:\n%s", view)
			}
		})
	}

	t.Run("only that chord opens it", func(t *testing.T) {
		p, _ := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
		drivePicker(p, altKey('x'))
		drivePicker(p, ctrlKey('c'))
		if p.ConfigModalOpen() {
			t.Fatal("a chord that is not alt+c opened the Config dashboard")
		}
	})

	t.Run("a picker with no opener ignores it", func(t *testing.T) {
		p := sizedPicker(worktreePickerOpts()...)
		if quit := drivePicker(p, altKey('c')); quit {
			t.Fatal("the chord quit a picker that hosts nothing")
		}
		if p.ConfigModalOpen() {
			t.Fatal("a picker with no opener opened something")
		}
	})
}

// The keys that would destroy a checkout. The control run is the point: the same
// press on the same picker hands cmd/worktree.go the worktree to delete.
func TestPickerConfigDashboardSuspendsTheDeleteKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want Action
	}{
		{"delete", ctrlKey('d'), ActionDelete},
		{"force delete", ctrlKey('x'), ActionForceDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control, _ := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
			if quit := drivePicker(control, tc.key); !quit {
				t.Fatalf("%s did not act on a picker with no modal open", tc.name)
			}
			if got := control.Result(); got.Action != tc.want || got.Selected == nil {
				t.Fatalf("control result = %+v, want %v on a worktree", got, tc.want)
			}

			p, writer := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
			drivePicker(p, altKey('c'))
			if quit := drivePicker(p, tc.key); quit {
				t.Fatalf("%s quit the picker while the modal was open", tc.name)
			}
			if got := p.Result(); got.Action != ActionConfirm || got.Selected != nil {
				t.Fatalf("%s reached the picker while the modal was open: %+v", tc.name, got)
			}
			if !p.ConfigModalOpen() {
				t.Fatalf("%s closed the modal", tc.name)
			}
			if tc.name == "force delete" && len(writer.removed) != 1 {
				t.Fatalf("ctrl+x removed %d overrides, want the one the component binds it to", len(writer.removed))
			}
		})
	}
}

// Selection, yank and cancel: each one ends the picker when the modal is closed,
// and none of them is even seen while it is open.
func TestPickerConfigDashboardSuspendsEveryOtherKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want Action
	}{
		{"select", enterKey, ActionConfirm},
		{"yank", ctrlKey('y'), ActionYankPath},
		{"kill session", ctrlKey('k'), ActionKillSession},
		{"cancel", escKey, ActionCancel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control, _ := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
			if quit := drivePicker(control, tc.key); !quit {
				t.Fatalf("%s did not act on a picker with no modal open", tc.name)
			}
			if got := control.Result().Action; got != tc.want {
				t.Fatalf("control action = %v, want %v", got, tc.want)
			}

			p, _ := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
			drivePicker(p, altKey('c'))
			if quit := drivePicker(p, tc.key); quit {
				t.Fatalf("%s quit the picker while the modal was open", tc.name)
			}
			if got := p.Result(); got.Action != ActionConfirm || got.Selected != nil {
				t.Fatalf("%s reached the picker while the modal was open: %+v", tc.name, got)
			}
			// esc is the component's own close, which is not the picker acting on it.
			if tc.name != "cancel" && !p.ConfigModalOpen() {
				t.Fatalf("%s closed the modal", tc.name)
			}
		})
	}
}

// Closing the modal puts the human back exactly where they were, and the
// selection they came to make still completes.
func TestPickerConfigDashboardReturnsTheStateItFound(t *testing.T) {
	p, writer := pickerHosting(t, &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}, worktreePickerOpts()...)
	typeIntoPicker(p, "fea")
	drivePicker(p, tea.KeyPressMsg{Code: tea.KeyUp})
	filter, cursor := p.input.Value(), p.cursor
	before := p.View().Content

	drivePicker(p, altKey('c'))
	drivePicker(p, tea.KeyPressMsg{Code: tea.KeyDown})
	drivePicker(p, enterKey)    // an edit, through the scripted editor
	typeIntoPicker(p, "verify") // filtering inside the component, not the picker
	drivePicker(p, escKey)

	if p.ConfigModalOpen() {
		t.Fatal("esc left the modal open")
	}
	if len(writer.stored) != 1 {
		t.Fatalf("the cycle stored %d overrides, want the one it edited", len(writer.stored))
	}
	if p.input.Value() != filter {
		t.Errorf("filter = %q after the modal, want the %q it was left on", p.input.Value(), filter)
	}
	if p.cursor != cursor {
		t.Errorf("cursor = %d after the modal, want the %d it was left on", p.cursor, cursor)
	}
	if got := p.View().Content; got != before {
		t.Errorf("the picker came back changed:\n%s\nwant:\n%s", got, before)
	}

	if quit := drivePicker(p, enterKey); !quit {
		t.Fatal("the picker did not complete its selection after the modal")
	}
	got := p.Result()
	if got.Action != ActionConfirm || got.Selected == nil || got.Selected.Path != "/repo-feature" {
		t.Fatalf("result = %+v, want the filtered worktree confirmed", got)
	}
}

// The stdout contract: the documented worktree binding substitutes this result
// into a shell `cd`, so a full open-edit-close cycle must leave both the bytes
// on stdout and the result itself exactly as a run without the modal.
func TestPickerConfigDashboardLeavesStdoutToThePicker(t *testing.T) {
	for name, opts := range map[string][]PickerOption{
		"worktree picker": worktreePickerOpts(),
		"project picker":  projectPickerOpts(),
	} {
		t.Run(name, func(t *testing.T) {
			var control, hosted Result
			out := captureStdout(t, func() {
				p, _ := pickerHosting(t, &scriptedEditor{}, opts...)
				typeIntoPicker(p, "fea")
				drivePicker(p, enterKey)
				control = p.Result()

				p, writer := pickerHosting(t, &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}, opts...)
				typeIntoPicker(p, "fea")
				drivePicker(p, altKey('c'))
				drivePicker(p, enterKey)     // edit
				drivePicker(p, ctrlKey('y')) // copy the source down
				drivePicker(p, escKey)
				drivePicker(p, enterKey)
				hosted = p.Result()
				if len(writer.stored) != 1 || len(writer.copied) != 1 {
					t.Fatalf("the cycle wrote %d edits and %d copies, want one of each", len(writer.stored), len(writer.copied))
				}
			})

			if out != "" {
				t.Fatalf("the cycle wrote to stdout: %q", out)
			}
			if control.Selected == nil || hosted.Selected == nil ||
				control.Selected.Path != hosted.Selected.Path || control.Action != hosted.Action {
				t.Fatalf("result after the modal = %+v, want the %+v a run without it produced", hosted, control)
			}
		})
	}
}

func TestPickerConfigDashboardInHelp(t *testing.T) {
	p, _ := pickerHosting(t, &scriptedEditor{}, worktreePickerOpts()...)
	if !hasHelpEntry(p.helpEntries(), ConfigDashboardKeyLabel) {
		t.Errorf("help lists no %s entry:\n%v", ConfigDashboardKeyLabel, p.helpEntries())
	}

	bare := sizedPicker(worktreePickerOpts()...)
	if hasHelpEntry(bare.helpEntries(), ConfigDashboardKeyLabel) {
		t.Errorf("a picker that hosts nothing advertises %s", ConfigDashboardKeyLabel)
	}

	// A user-defined command on the same chord keeps it, as it does for every
	// other built-in key here.
	taken, _ := pickerHosting(t, &scriptedEditor{}, WithUserDefinedCommands([]UserDefinedCommand{
		{Key: ConfigDashboardKey, Label: "mine", Command: "echo mine"},
	}))
	// The human's own label is on the chord now, so what must be gone is this
	// component's entry, not the key.
	if hasHelpEntry(taken.helpEntries(), ConfigDashboardKeyLabel, "Config overrides") {
		t.Errorf("a user-bound alt+c still advertises the Config dashboard")
	}
	if quit := drivePicker(taken, altKey('c')); !quit {
		t.Fatal("a user-bound alt+c did not run the human's own command")
	}
	if taken.ConfigModalOpen() {
		t.Fatal("a user-bound alt+c opened the Config dashboard instead")
	}
}

// hasHelpEntry finds an entry by key, and by label too when one is named — a
// user-defined command can hold the same key with a label of its own.
func hasHelpEntry(entries []HelpEntry, key string, desc ...string) bool {
	for _, e := range entries {
		if e.Key != key {
			continue
		}
		if len(desc) == 0 || e.Desc == desc[0] {
			return true
		}
	}
	return false
}
