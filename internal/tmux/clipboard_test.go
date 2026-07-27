package tmux

import (
	"reflect"
	"testing"
)

func TestLoadBufferBuildsArgsAndStreamsText(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.LoadBuffer("boom\n\nstack trace"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantArgs := []inputCall{{text: "boom\n\nstack trace", args: []string{"load-buffer", "-w", "-"}}}
	if !reflect.DeepEqual(r.inputCalls, wantArgs) {
		t.Fatalf("inputCalls = %+v, want %+v", r.inputCalls, wantArgs)
	}
}
