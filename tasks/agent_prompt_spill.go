package tasks

import (
	"fmt"
	"os"
	"strings"
)

// A generated prompt does not ride in argv. execve caps the whole argument
// vector (1 MiB on darwin), and a prompt assembled from a set's criteria, task
// bodies and spec runs to hundreds of kilobytes — a Verifier prompt for a large
// set has reached megabytes — so the run dies as `argument list too long` before
// the agent starts, naming a limit the human cannot act on.
//
// Every headless invocation therefore spills its prompt to a file just before it
// spawns and passes a short instruction naming that file. The spill sits at the
// run seam, so it lifts the ceiling for every preset at once: no per-agent stdin
// capability to declare, no adapter-by-adapter rollout. The file lives for the
// length of one attempt — a retry spills again — and is removed however the
// attempt ends, including a timeout or an interrupt.

// spillPrompt moves the generated prompt out of argv into a temporary file,
// replacing the argument with an instruction to read it. It is idempotent and a
// no-op for an invocation that carries no generated prompt (a preset launched
// bare) or an empty one.
func (i *AgentInvocation) spillPrompt() error {
	if i == nil || i.promptFile != "" {
		return nil
	}
	// A generated prompt is never argv[0]: the adapters put it after at least the
	// headless prefix, so index 0 means "this invocation carries none".
	if i.promptArg <= 0 || i.promptArg >= len(i.Args) {
		return nil
	}
	prompt := i.Args[i.promptArg]
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	f, err := os.CreateTemp("", "pop-agent-prompt-*.md")
	if err != nil {
		return fmt.Errorf("write agent prompt file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		os.Remove(path)
		return fmt.Errorf("write agent prompt file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("write agent prompt file %s: %w", path, err)
	}
	i.Args[i.promptArg] = promptFileInstruction(path)
	i.promptFile = path
	return nil
}

// cleanupPrompt removes the spill file. Called however an attempt ended, so a
// timed-out or interrupted run leaves nothing behind.
func (i *AgentInvocation) cleanupPrompt() {
	if i == nil || i.promptFile == "" {
		return
	}
	os.Remove(i.promptFile)
	i.promptFile = ""
}

// promptFileInstruction is what argv carries in the prompt's place: enough for
// any agent to fetch the real instructions, and short enough that argv size no
// longer depends on prompt size.
func promptFileInstruction(path string) string {
	return fmt.Sprintf("Read the file %s in full: it holds your complete instructions for this run. Follow them exactly. Nothing else was sent to you.", path)
}
