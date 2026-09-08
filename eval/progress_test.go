package main

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEvalProgressTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func (t *fakeEvalProgressTicker) Chan() <-chan time.Time { return t.ticks }
func (t *fakeEvalProgressTicker) Stop() {
	t.once.Do(func() { close(t.stopped) })
}

func TestEvalProgressReportsWaitingAtThirtySecondIntervals(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	started := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ticker := &fakeEvalProgressTicker{ticks: make(chan time.Time, 1), stopped: make(chan struct{})}
	progress := evalProgress{
		out: writer,
		now: func() time.Time { return started },
		newTicker: func(interval time.Duration) evalProgressTicker {
			if interval != 30*time.Second {
				t.Fatalf("interval = %s", interval)
			}
			return ticker
		},
	}
	stop := progress.wait("Case=example Arm=bare repeat=2", "Bare-agent invocation", 4*time.Hour)
	for _, elapsed := range []time.Duration{30 * time.Second, 60 * time.Second} {
		ticker.ticks <- started.Add(elapsed)
		want := "Trial Case=example Arm=bare repeat=2 waiting: phase=Bare-agent invocation elapsed=" + elapsed.String() + " ceiling=4h0m0s"
		if got := <-lines; got != want {
			t.Fatalf("line = %q; want %q", got, want)
		}
	}
	stop()
	<-ticker.stopped
	ticker.ticks <- started.Add(90 * time.Second)
	select {
	case line := <-lines:
		t.Fatalf("line after stop: %q", line)
	default:
	}
	_ = writer.Close()
}

func TestEvalWaitingLinesAreBoundedAndDoNotInventACeiling(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	started := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ticker := &fakeEvalProgressTicker{ticks: make(chan time.Time, 1), stopped: make(chan struct{})}
	progress := evalProgress{
		out:       writer,
		now:       func() time.Time { return started },
		newTicker: func(time.Duration) evalProgressTicker { return ticker },
	}
	stop := progress.wait("Case=example Arm=pop repeat=1", "Objective gate\n"+strings.Repeat("long ", 40), 0)
	ticker.ticks <- started.Add(30 * time.Second)
	want := "Trial Case=example Arm=pop repeat=1 waiting: phase=Objective gate " + strings.Repeat("long ", 20) + "lo... elapsed=30s"
	if got := <-lines; got != want {
		t.Fatalf("line = %q; want %q", got, want)
	}
	stop()
	_ = writer.Close()
}

func recordingWaiter(waits *[]evalWait, stopped *int) func(evalWait) func() {
	return func(wait evalWait) func() {
		*waits = append(*waits, wait)
		return func() { (*stopped)++ }
	}
}

func waitPhases(waits []evalWait) []string {
	phases := make([]string, len(waits))
	for i, wait := range waits {
		phases[i] = wait.phase
	}
	return phases
}
