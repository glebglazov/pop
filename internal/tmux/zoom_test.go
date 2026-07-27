package tmux

import (
	"reflect"
	"testing"
)

func TestWindowZoomedBuildsArgsAndParses(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{out: "1", want: true},
		{out: "0", want: false},
	}
	for _, tt := range tests {
		r := &recordingRunner{out: tt.out}
		tm := &realTmux{run: r}

		got, err := tm.WindowZoomed("%5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantArgs := [][]string{{"display-message", "-t", "%5", "-p", "#{window_zoomed_flag}"}}
		if !reflect.DeepEqual(r.calls, wantArgs) {
			t.Fatalf("args = %v, want %v", r.calls, wantArgs)
		}
		if got != tt.want {
			t.Errorf("WindowZoomed(%q) = %v, want %v", tt.out, got, tt.want)
		}
	}
}

func TestZoomPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.ZoomPane("%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"resize-pane", "-Z", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}
