package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestErrorModelClipboardPayload(t *testing.T) {
	tests := []struct {
		name    string
		message string
		trace   string
		want    string
	}{
		{
			name:    "message only",
			message: "boom",
			trace:   "",
			want:    "boom",
		},
		{
			name:    "message with trace",
			message: "boom",
			trace:   "goroutine 1 [running]:\nmain.main()\n\t/tmp/x.go:10 +0x1",
			want:    "boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/tmp/x.go:10 +0x1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &errorModel{message: tt.message, trace: tt.trace}
			if got := m.clipboardPayload(); got != tt.want {
				t.Errorf("clipboardPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorModelCopyKeySucceeds(t *testing.T) {
	var captured string
	m := &errorModel{
		message: "failed to load config",
		trace:   "stack frame",
		copyFunc: func(s string) error {
			captured = s
			return nil
		},
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c'})

	if cmd != nil {
		t.Error("copy key should not quit the program")
	}
	if !m.copied {
		t.Error("expected copied=true after successful copy")
	}
	if m.copyErrMsg != "" {
		t.Errorf("expected empty copyErrMsg, got %q", m.copyErrMsg)
	}
	want := "failed to load config\n\nstack frame"
	if captured != want {
		t.Errorf("copyFunc called with %q, want %q", captured, want)
	}
}

func TestErrorModelCopyKeyFailurePreservesScreen(t *testing.T) {
	m := &errorModel{
		message: "boom",
		copyFunc: func(string) error {
			return errors.New("clipboard unavailable")
		},
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c'})

	if cmd != nil {
		t.Error("failed copy should not dismiss the screen")
	}
	if m.copied {
		t.Error("copied should remain false on failure")
	}
	if m.copyErrMsg != "clipboard unavailable" {
		t.Errorf("copyErrMsg = %q, want %q", m.copyErrMsg, "clipboard unavailable")
	}
}

func TestErrorModelAnyOtherKeyDismisses(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"q", tea.KeyPressMsg{Code: 'q'}},
		{"space", tea.KeyPressMsg{Code: ' '}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &errorModel{message: "boom"}
			_, cmd := m.Update(tt.msg)
			if cmd == nil {
				t.Errorf("key %s should return tea.Quit cmd", tt.name)
			}
		})
	}
}

func TestErrorModelCopyAfterFailureRecovers(t *testing.T) {
	callCount := 0
	m := &errorModel{
		message: "boom",
		copyFunc: func(string) error {
			callCount++
			if callCount == 1 {
				return errors.New("transient")
			}
			return nil
		},
	}

	// First press: fails
	m.Update(tea.KeyPressMsg{Code: 'c'})
	if m.copied || m.copyErrMsg == "" {
		t.Fatalf("expected failed state after first press, got copied=%v err=%q", m.copied, m.copyErrMsg)
	}

	// Second press: succeeds — should clear the error message
	m.Update(tea.KeyPressMsg{Code: 'c'})
	if !m.copied {
		t.Error("expected copied=true after successful retry")
	}
	if m.copyErrMsg != "" {
		t.Errorf("expected copyErrMsg to be cleared after success, got %q", m.copyErrMsg)
	}
}

func TestErrorModelViewRendersMessageAndTrace(t *testing.T) {
	m := &errorModel{
		message: "failed to load config: permission denied",
		trace:   "goroutine 1 [running]:\nmain.main()",
	}
	m.width = 80
	m.height = 24

	content := fmt.Sprint(m.View())
	// Strip ANSI so we assert on visible text only.
	plain := StripANSI(content)

	mustContain := []string{
		"Error",
		"failed to load config: permission denied",
		"Stack trace",
		"goroutine 1 [running]:",
		"main.main()",
		"c copy",
		"dismiss",
	}
	for _, want := range mustContain {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q\n---\n%s\n---", want, plain)
		}
	}
}

func TestErrorModelViewOmitsTraceSectionWhenEmpty(t *testing.T) {
	m := &errorModel{message: "boom"}
	m.width = 80
	m.height = 24

	plain := StripANSI(fmt.Sprint(m.View()))

	if strings.Contains(plain, "Stack trace") {
		t.Errorf("view should not render 'Stack trace' header when trace is empty:\n%s", plain)
	}
	if !strings.Contains(plain, "boom") {
		t.Errorf("view missing error message:\n%s", plain)
	}
}

func TestErrorModelViewShowsCopyStatus(t *testing.T) {
	m := &errorModel{message: "boom", copied: true}
	m.width = 80
	m.height = 24

	plain := StripANSI(fmt.Sprint(m.View()))
	if !strings.Contains(plain, "Copied to clipboard") {
		t.Errorf("view should show copied confirmation:\n%s", plain)
	}
}

func TestErrorModelViewShowsCopyFailure(t *testing.T) {
	m := &errorModel{message: "boom", copyErrMsg: "no tty"}
	m.width = 80
	m.height = 24

	plain := StripANSI(fmt.Sprint(m.View()))
	if !strings.Contains(plain, "Copy failed: no tty") {
		t.Errorf("view should show copy failure reason:\n%s", plain)
	}
}

func TestShowErrorNilIsNoop(t *testing.T) {
	// Should return immediately without starting a Bubbletea program.
	// If this hangs, the test times out and we know ShowError(nil) isn't handled.
	done := make(chan struct{})
	go func() {
		ShowError(nil, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ShowError(nil) should return immediately")
	}
}
