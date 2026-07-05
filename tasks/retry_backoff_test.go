package tasks

import (
	"bytes"
	"io"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

func TestAttemptRetryDelay(t *testing.T) {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}
	tests := []struct {
		failedAttempt int
		want          time.Duration
	}{
		{failedAttempt: 1, want: time.Minute},
		{failedAttempt: 2, want: 5 * time.Minute},
		{failedAttempt: 3, want: 15 * time.Minute},
		{failedAttempt: 4, want: 15 * time.Minute},
	}
	for _, tt := range tests {
		if got := attemptRetryDelay(delays, tt.failedAttempt); got != tt.want {
			t.Fatalf("attempt %d delay = %s, want %s", tt.failedAttempt, got, tt.want)
		}
	}
	if got := attemptRetryDelay(nil, 1); got != 0 {
		t.Fatalf("nil delays = %s, want 0", got)
	}
}

func TestResolveImplementMaxTries(t *testing.T) {
	five := 5
	ten := 10
	cfg := &config.Config{Task: &config.TasksConfig{
		MaxTries: &ten,
		Implement: &config.ImplementConfig{MaxTries: &five},
	}}

	if got, err := resolveImplementMaxTries(cfg, true, 2); err != nil || got != 2 {
		t.Fatalf("explicit flag = (%d, %v), want (2, nil)", got, err)
	}
	if got, err := resolveImplementMaxTries(cfg, false, 0); err != nil || got != 5 {
		t.Fatalf("config implement override = (%d, %v), want (5, nil)", got, err)
	}
	if got, err := resolveImplementMaxTries(&config.Config{Task: &config.TasksConfig{MaxTries: &ten}}, false, 0); err != nil || got != 10 {
		t.Fatalf("config root cap = (%d, %v), want (10, nil)", got, err)
	}
	if got, err := resolveImplementMaxTries(nil, false, 7); err != nil || got != 7 {
		t.Fatalf("no config with flag value = (%d, %v), want (7, nil)", got, err)
	}
	if got, err := resolveImplementMaxTries(nil, false, 0); err != nil || got != config.DefaultTaskMaxTries {
		t.Fatalf("default = (%d, %v), want (%d, nil)", got, err, config.DefaultTaskMaxTries)
	}
}

func TestResolveAttemptRetryDelaysFromConfig(t *testing.T) {
	got, err := resolveAttemptRetryDelays(&config.Config{Task: &config.TasksConfig{
		AttemptRetryDelays: []string{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty list = %#v, want []", got)
	}

	want := append([]time.Duration(nil), config.DefaultTaskAttemptRetryDelays...)
	got, err = resolveAttemptRetryDelays(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %#v, want %#v", got, want)
	}
}

func TestWaitAttemptRetryDelayCompletes(t *testing.T) {
	var slept atomic.Int64
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	waiter := retryWaiter{
		now: func() time.Time { return now },
		sleep: func(d time.Duration) {
			slept.Add(int64(d))
			now = now.Add(d)
		},
	}
	var buf bytes.Buffer
	if interrupted := waitAttemptRetryDelay(&buf, 3*time.Second, waiter); interrupted {
		t.Fatal("expected completion, got interrupted")
	}
	if got := slept.Load(); got != int64(3*time.Second) {
		t.Fatalf("slept = %d, want %d", got, 3*time.Second)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Retrying with preserved changes in")) {
		t.Fatalf("missing countdown output:\n%s", buf.String())
	}
}

func TestRunTaskInterruptedDuringRetryWait(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask:    true,
		skipSentinel: true,
	})

	retryDelayWaitHook = func(out io.Writer, delay time.Duration, _ retryWaiter) bool {
		return true
	}
	t.Cleanup(func() { retryDelayWaitHook = testRetryDelayWaitHook })

	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{
			AttemptRetryDelays: []string{"1m"},
		}}, nil
	}

	opts := env.runOpts(true, agent)
	opts.MaxTries = 2
	opts.Output = io.Discard

	_, err := RunTaskWith(env.deps(), nil, loadConfig, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskInstantRetriesWithEmptyDelayList(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{checkTask: true, skipSentinel: true},
		{checkTask: true, summary: "done on retry"},
	})

	var calls int32
	runner := &countingRunner{t: t, calls: &calls, exitCode: 0}
	d := env.deps()
	d.Runner = runner

	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{
			AttemptRetryDelays: []string{},
		}}, nil
	}

	opts := env.runOpts(true, agent)
	opts.MaxTries = 2
	opts.Output = io.Discard

	start := time.Now()
	_, err := RunTaskWith(d, nil, loadConfig, opts)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("started attempts = %d, want 2", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("instant retries took %s, want immediate", elapsed)
	}
}

func TestRunTaskConfigMaxTriesWithoutExplicitFlag(t *testing.T) {
	env := setupExecutorFixture(t, false)
	var calls int32
	runner := &countingRunner{t: t, calls: &calls, exitCode: 1}
	d := env.deps()
	d.Runner = runner

	five := 5
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{
			Implement: &config.ImplementConfig{MaxTries: &five},
			AttemptRetryDelays: []string{},
		}}, nil
	}

	opts := env.runOpts(true, writeFakeAgent(t, env.root, fakeAgentConfig{exitCode: 1}))
	opts.MaxTries = 3
	opts.MaxTriesExplicit = false
	opts.Output = io.Discard

	_, err := RunTaskWith(d, nil, loadConfig, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskFailed(t, env, "01-a", 5)
}
