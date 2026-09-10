package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

// Setup runs on every apply, before window inspection or realization. The
// Workbench author owns idempotency of these side effects (ADR-0075).
func runWorkbenchSetup(d templateRuntimeDeps, entries config.SetupEntries, dir string) error {
	if d.RunBeforeApply == nil {
		return nil
	}
	out := d.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := d.ErrOut
	if errOut == nil {
		errOut = os.Stderr
	}
	for i, entry := range entries {
		if entry.Parallel != nil {
			if err := runSetupGroup(d, entry.Parallel, dir, out); err != nil {
				return fmt.Errorf("before_apply[%d]: %w", i, err)
			}
		} else if err := d.RunBeforeApply(d.Tmux, entry.Command, dir, os.Stdin, out, errOut); err != nil {
			return fmt.Errorf("before_apply[%d] %q failed: %w", i, entry.Command, err)
		}
	}
	return nil
}

func runSetupGroup(d templateRuntimeDeps, commands []string, dir string, out io.Writer) error {
	return runSetupGroupWithProgress(d, commands, dir, newSetupProgress(out, commands, setupProgressDeps{}))
}

const setupProgressInterval = 100 * time.Millisecond

type setupProgressTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realSetupProgressTicker struct{ *time.Ticker }

func (t realSetupProgressTicker) Chan() <-chan time.Time { return t.C }

type setupProgressDeps struct {
	now       func() time.Time
	newTicker func(time.Duration) setupProgressTicker
	terminal  func(io.Writer) bool
}

type setupCommandProgress struct {
	command  string
	finished time.Time
	failed   bool
}

type setupProgress struct {
	out      io.Writer
	terminal bool
	now      func() time.Time
	ticker   func(time.Duration) setupProgressTicker
	started  time.Time
	commands []setupCommandProgress
}

func newSetupProgress(out io.Writer, commands []string, deps setupProgressDeps) *setupProgress {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	newTicker := deps.newTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) setupProgressTicker {
			return realSetupProgressTicker{time.NewTicker(interval)}
		}
	}
	isTerminal := deps.terminal
	if isTerminal == nil {
		isTerminal = setupWriterIsTerminal
	}
	progress := &setupProgress{out: out, terminal: isTerminal(out), now: now, ticker: newTicker}
	for _, command := range commands {
		progress.commands = append(progress.commands, setupCommandProgress{command: command})
	}
	return progress
}

func setupWriterIsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func (p *setupProgress) start() {
	p.started = p.now()
	if p.terminal {
		p.refresh(p.started, false)
		return
	}
	for _, command := range p.commands {
		fmt.Fprintf(p.out, "Setup: %s [running 0s]\n", command.command)
	}
}

func (p *setupProgress) finish(index int, failed bool) {
	p.commands[index].finished = p.now()
	p.commands[index].failed = failed
	if p.terminal {
		p.refresh(p.commands[index].finished, false)
		return
	}
	state := "finished"
	if failed {
		state = "failed"
	}
	fmt.Fprintf(p.out, "Setup: %s [%s %s]\n", p.commands[index].command, state, setupElapsed(p.commands[index].finished.Sub(p.started)))
}

func (p *setupProgress) refresh(now time.Time, final bool) {
	parts := make([]string, 0, len(p.commands))
	for _, command := range p.commands {
		state := "running"
		at := now
		if !command.finished.IsZero() {
			state = "finished"
			at = command.finished
			if command.failed {
				state = "failed"
			}
		}
		parts = append(parts, fmt.Sprintf("%s [%s %s]", command.command, state, setupElapsed(at.Sub(p.started))))
	}
	fmt.Fprintf(p.out, "\r\033[2KSetup: %s", strings.Join(parts, " | "))
	if final {
		fmt.Fprintln(p.out)
	}
}

func setupElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed.Round(setupProgressInterval).String()
}

func runSetupGroupWithProgress(d templateRuntimeDeps, commands []string, dir string, progress *setupProgress) error {
	type result struct {
		output bytes.Buffer
		err    error
	}
	results := make([]result, len(commands))
	completed := make(chan int, len(commands))
	progress.start()
	for i, command := range commands {
		go func() {
			r := &results[i]
			// One shared writer lets os/exec serialize stdout and stderr for this
			// command, while each sibling owns a separate buffer.
			r.err = d.RunBeforeApply(d.Tmux, command, dir, nil, &r.output, &r.output)
			completed <- i
		}()
	}
	var ticker setupProgressTicker
	var ticks <-chan time.Time
	if progress.terminal {
		ticker = progress.ticker(setupProgressInterval)
		ticks = ticker.Chan()
		defer ticker.Stop()
	}
	for completedCount := 0; completedCount < len(commands); {
		select {
		case tick := <-ticks:
			progress.refresh(tick, false)
		case i := <-completed:
			progress.finish(i, results[i].err != nil)
			completedCount++
		}
	}
	if progress.terminal {
		progress.refresh(progress.now(), true)
	}
	failed := -1
	for i := range results {
		if results[i].err != nil {
			failed = i
			break
		}
	}
	if failed >= 0 {
		_, _ = progress.out.Write(results[failed].output.Bytes())
	}
	for i := range results {
		if i != failed {
			_, _ = progress.out.Write(results[i].output.Bytes())
		}
	}
	if failed >= 0 {
		return fmt.Errorf("parallel[%d] %q failed: %w", failed, commands[failed], results[failed].err)
	}
	return nil
}
