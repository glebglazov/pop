package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"

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
	type result struct {
		output bytes.Buffer
		err    error
	}
	results := make([]result, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := &results[i]
			// One shared writer lets os/exec serialize stdout and stderr for this
			// command, while each sibling owns a separate buffer.
			r.err = d.RunBeforeApply(d.Tmux, command, dir, nil, &r.output, &r.output)
		}()
	}
	wg.Wait()
	failed := -1
	for i := range results {
		if results[i].err != nil {
			failed = i
			break
		}
	}
	if failed >= 0 {
		_, _ = out.Write(results[failed].output.Bytes())
	}
	for i := range results {
		if i != failed {
			_, _ = out.Write(results[i].output.Bytes())
		}
	}
	if failed >= 0 {
		return fmt.Errorf("parallel[%d] %q failed: %w", failed, commands[failed], results[failed].err)
	}
	return nil
}
