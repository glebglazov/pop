package tasks

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/glebglazov/pop/config"
)

// attemptRetryDelay returns the wait before the next attempt after failedAttempt
// (1-based) fails. The last delay repeats when failedAttempt exceeds the list.
func attemptRetryDelay(delays []time.Duration, failedAttempt int) time.Duration {
	if len(delays) == 0 || failedAttempt < 1 {
		return 0
	}
	idx := failedAttempt - 1
	if idx >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[idx]
}

func resolveVerifyMaxTries(cfg *config.Config) (int, error) {
	if cfg != nil {
		return cfg.ResolveVerifyMaxTries(), nil
	}
	return config.DefaultTaskMaxTries, nil
}

func resolveImplementMaxTries(cfg *config.Config, explicit bool, flagValue int) (int, error) {
	if explicit {
		if flagValue > 0 {
			return flagValue, nil
		}
		return config.DefaultTaskMaxTries, nil
	}
	if cfg != nil {
		return cfg.ResolveImplementMaxTries(), nil
	}
	if flagValue > 0 {
		return flagValue, nil
	}
	return config.DefaultTaskMaxTries, nil
}

func resolveAttemptRetryDelays(cfg *config.Config) ([]time.Duration, error) {
	if cfg == nil {
		return append([]time.Duration(nil), config.DefaultTaskAttemptRetryDelays...), nil
	}
	return cfg.ResolveAttemptRetryDelays()
}

type retryWaiter struct {
	now   func() time.Time
	sleep func(time.Duration)
}

func defaultRetryWaiter() retryWaiter {
	return retryWaiter{now: time.Now, sleep: time.Sleep}
}

// retryDelayWaitHook, when set by tests, replaces waitAttemptRetryDelay.
var retryDelayWaitHook func(out io.Writer, delay time.Duration, waiter retryWaiter) bool

func waitRetryDelay(out io.Writer, delay time.Duration) bool {
	if retryDelayWaitHook != nil {
		return retryDelayWaitHook(out, delay, defaultRetryWaiter())
	}
	return waitAttemptRetryDelay(out, delay, defaultRetryWaiter())
}

// waitAttemptRetryDelay sleeps for delay, printing a countdown to out. Returns
// true when interrupted by SIGINT/SIGTERM (Ctrl-C during the wait).
func waitAttemptRetryDelay(out io.Writer, delay time.Duration, waiter retryWaiter) bool {
	if delay <= 0 {
		return false
	}
	if waiter.now == nil {
		waiter = defaultRetryWaiter()
	}
	if waiter.sleep == nil {
		waiter.sleep = time.Sleep
	}

	display := outputFor(out)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	deadline := waiter.now().Add(delay)
	for {
		remaining := deadline.Sub(waiter.now())
		if remaining <= 0 {
			fmt.Fprintln(display)
			return false
		}
		fmt.Fprintf(display, "\r%s", display.styled(ansiYellow,
			fmt.Sprintf("↻ Retrying with preserved changes in %s...", formatRetryCountdown(remaining))))

		wait := time.Second
		if remaining < wait {
			wait = remaining
		}

		done := make(chan struct{})
		go func(d time.Duration) {
			waiter.sleep(d)
			close(done)
		}(wait)

		select {
		case <-sigCh:
			fmt.Fprintln(display)
			return true
		case <-done:
		}
	}
}

func formatRetryCountdown(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm%ds", mins, secs)
}
