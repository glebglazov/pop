package routine

import (
	"strings"
	"testing"
)

func TestRefinePaneWithFreshSpawn(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "fresh", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if err := RefinePaneWith(d, "fresh", ""); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	rt := tmuxRecorder(d)
	newWindow, ok := rt.findCommand("new-window")
	if !ok {
		t.Fatal("expected new-window")
	}
	if !argPresent(newWindow, "-d") || !containsArg(newWindow, "-n", "fresh") {
		t.Fatalf("new-window = %v, want detached -n fresh", newWindow)
	}
	if !containsArg(newWindow, "-c", home) {
		t.Fatalf("new-window = %v, want -c %q", newWindow, home)
	}
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected send-keys")
	}
	joined := strings.Join(sendKeys, " ")
	if !strings.Contains(joined, "/mock/bin/pop routine edit fresh") {
		t.Fatalf("send-keys = %v, want resolved executable", sendKeys)
	}
	if _, ok := rt.findCommand("new-session"); !ok {
		t.Fatal("expected new-session when absent")
	}
}

func TestRefinePaneWithExistingWindowSendsNothing(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "live", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	rt := newRecordingTmux(true, "live")
	rt.paneList = "%5"
	d.Tmux = rt
	if err := RefinePaneWith(d, "live", ""); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	if _, ok := rt.findCommand("send-keys"); ok {
		t.Fatalf("must not send-keys into live window, commands=%v", rt.commands)
	}
	if _, ok := rt.findCommand("new-window"); ok {
		t.Fatalf("must not create window, commands=%v", rt.commands)
	}
	if switchClient, ok := rt.findCommand("switch-client"); !ok || !containsArg(switchClient, "-t", "%5") {
		t.Fatalf("expected switch-client to %%5, commands=%v", rt.commands)
	}
}

func TestRefinePaneWithForwardsRefineAgent(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "fwd", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if err := RefinePaneWith(d, "fwd", "claude"); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	rt := tmuxRecorder(d)
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected send-keys")
	}
	joined := strings.Join(sendKeys, " ")
	if !strings.Contains(joined, "--refine-agent claude") {
		t.Fatalf("send-keys = %v, want --refine-agent claude", sendKeys)
	}
}

func TestRefinePaneWithOutsideTmuxRefuses(t *testing.T) {
	d, home := routineDashboardDeps(t)
	d.InTmux = func() bool { return false }
	if _, err := AddWith(d, "cli", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	err := RefinePaneWith(d, "cli", "codex")
	if err == nil {
		t.Fatal("expected refusal outside tmux")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pop routine edit cli") {
		t.Fatalf("refusal = %q, want CLI equivalent", msg)
	}
	if !strings.Contains(msg, "--refine-agent codex") {
		t.Fatalf("refusal = %q, want refine-agent in suggested command", msg)
	}
	rt := tmuxRecorder(d)
	if len(rt.commands) != 0 {
		t.Fatalf("must not touch tmux, commands=%v", rt.commands)
	}
}
