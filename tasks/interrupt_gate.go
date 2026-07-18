package tasks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// interruptGateExit terminates the process on the interrupt gate's force-quit — a
// second SIGINT while the menu is displayed. A package var so tests can observe
// the force-quit without killing the test binary.
var interruptGateExit = func(code int) { os.Exit(code) }

type interruptGateAction int

const (
	// interruptGateExitChoice is menu "0": finalize the drain with the interrupted
	// terminal (today's teardown-and-exit behavior).
	interruptGateExitChoice interruptGateAction = iota
	// interruptGateContinueChoice is menu "1": re-acquire the lock and re-run the
	// interrupted task, then keep draining.
	interruptGateContinueChoice
	// interruptGateForceQuit is a second SIGINT at the menu: exit the process
	// immediately, bypassing the clean park-and-resume choreography.
	interruptGateForceQuit
)

type interruptReadResult struct {
	answer string
	err    error
}

// handleInteractiveInterruptGate is the fourth sibling of the HITL / Failed /
// Verify-fail gate menus (ADR-0119): when a live AFK attempt is torn down by
// SIGINT on a TTY, the drain lands here instead of exiting 130. It offers two
// options — 1 Continue draining (re-run the interrupted task) and 0 Exit
// (finalize with the interrupted terminal). A second SIGINT while the menu is up
// force-quits the process immediately (interruptGateExit). Returns (true, nil)
// when the caller should keep draining (Continue) and (false, nil) when it should
// fall through to the interrupted terminal (Exit, or a non-promptable run under
// --yes / a non-interactive input). sigCh delivers the second SIGINT; the caller
// installs and stops the signal notification around this call.
func handleInteractiveInterruptGate(env gateEnv, m *Manifest, interrupted *Task, sigCh <-chan os.Signal) (bool, error) {
	out := env.out
	in := env.in
	reader := env.reader
	taskSetID := env.taskSetID
	if env.yes || !canPrompt(in) || m == nil || interrupted == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	for {
		action, err := promptInterruptGateAction(out, reader, sigCh, taskSetID, interrupted)
		if err != nil {
			return false, err
		}
		switch action {
		case interruptGateContinueChoice:
			return true, nil
		case interruptGateExitChoice:
			return false, nil
		case interruptGateForceQuit:
			interruptGateExit(ExitInterrupted)
			// interruptGateExit terminates in production; only a test override
			// returns, in which case a force-quit resolves like Exit.
			return false, nil
		}
	}
}

func promptInterruptGateAction(out io.Writer, reader *bufio.Reader, sigCh <-chan os.Signal, taskSetID string, interrupted *Task) (interruptGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiYellow, "Interrupted: %s/%s was stopped mid-run.", taskSetID, interrupted.ID)
	fmt.Fprintln(display, "  1. Continue draining (default)")
	fmt.Fprintln(display, "  0. Exit")
	fmt.Fprintln(display, "  (press Ctrl-C again to force-quit)")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	// A second SIGINT while the menu is up is the force-quit escape hatch: catch
	// one already pending before blocking on input.
	select {
	case <-sigCh:
		return interruptGateForceQuit, nil
	default:
	}

	// Read the selection in a goroutine so a SIGINT arriving mid-read still
	// force-quits rather than waiting for a line that may never come.
	lineCh := make(chan interruptReadResult, 1)
	go func() {
		answer, err := readPromptLine(reader, "0")
		lineCh <- interruptReadResult{answer: answer, err: err}
	}()

	select {
	case <-sigCh:
		return interruptGateForceQuit, nil
	case res := <-lineCh:
		if res.err != nil {
			return interruptGateExitChoice, res.err
		}
		switch strings.ToLower(strings.TrimSpace(res.answer)) {
		case "", "1", "c", "continue":
			return interruptGateContinueChoice, nil
		case "0", "q", "quit", "exit":
			return interruptGateExitChoice, nil
		default:
			fmt.Fprintln(display, "Choose 1 or 0.")
			return promptInterruptGateAction(out, reader, sigCh, taskSetID, interrupted)
		}
	}
}

// interruptGate runs the interrupt gate choreography (ADR-0119) for the drain
// loop: when a live AFK attempt is torn down by SIGINT, park the Runtime
// execution lock (registering a checkout gate hold) so the menu runs lock-free,
// present the Continue/Exit menu, then re-acquire the lock. It reuses the exact
// gate lock discipline of hitlGate/failedGate — park lock + gate hold, menu
// lock-free, resume re-acquires (ADR-0067) — rather than introducing a new pause
// mechanism. Returns (true, nil) when Continue was chosen and the loop should
// keep draining (the interrupted task is still open, so the loop re-picks it and
// the resume path carries its digest forward, ADR-0091); (false, nil) when Exit
// was chosen, so the caller returns the interrupt error and the deferred finalize
// stamps the interrupted terminal. Under --yes or a non-interactive input the
// gate does not prompt — it leaves the Drain held and returns (false, nil) so the
// caller keeps today's teardown-and-exit.
func (r *implementRun) interruptGate(m *Manifest, interrupted *Task) (bool, error) {
	if !gateWillPrompt(r.opts.ConfirmIn, r.opts.Yes, m, interrupted) {
		return false, nil
	}
	r.sharedPromptReader = ensurePromptReader(r.sharedPromptReader, r.opts.ConfirmIn, r.opts.Yes)

	// Watch for the second SIGINT for the duration of the gate. Installed before
	// the park so a signal arriving during the park write is caught and resolved
	// as a force-quit at the menu rather than hitting the default (fatal) action.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	r.parkAtGate(m, interrupted)
	cont, err := handleInteractiveInterruptGate(r.newGateEnv(), m, interrupted, sigCh)
	r.releaseGateHold()
	if err != nil {
		return cont, err
	}
	// Re-acquire the lock the park released (ADR-0067): Continue resumes the
	// interrupted task, Exit re-holds so the deferred finalize records the
	// interrupted terminal (today's behavior). A collision refuses cleanly.
	if derr := r.ensureDrain(); derr != nil {
		return cont, derr
	}
	return cont, nil
}
