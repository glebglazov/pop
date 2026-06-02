package workload

import (
	"bytes"
	"strings"
	"testing"
)

func TestOutputForBufferDoesNotEmitANSI(t *testing.T) {
	var buf bytes.Buffer
	out := outputFor(&buf)
	out.line(ansiGreen, "done")
	if got := buf.String(); strings.Contains(got, "\033[") {
		t.Fatalf("redirected output contains ANSI: %q", got)
	}
}

func TestStyledTableUsesSemanticColor(t *testing.T) {
	var buf bytes.Buffer
	out := &output{Writer: &buf, color: true}
	render(out, &RefreshResult{Rows: []Row{{ID: "demo", Status: StatusReady}}})
	if got := buf.String(); !strings.Contains(got, ansiCyan) {
		t.Fatalf("styled READY row missing cyan: %q", got)
	}
}

func TestColorEnabledHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(true) {
		t.Fatal("color enabled with NO_COLOR set")
	}
}
