# A Workbench runs its setup in the human's shell, in declared groups

A **Workbench**'s **Setup command**s (`before_apply`) ran one at a time, in order,
each through a bare `sh -c`. Both halves of that were wrong for the same reason:
they answer questions the human had already answered in the file. Two installs
that share no dependency were forced into a sequence, and a command written in
the human's own vocabulary could not resolve, because the shell pop chose reads
none of the human's configuration. A Workbench therefore spoke two dialects —
its pane commands ran in the human's shell, its setup did not — and the trap is
found by writing a Workbench, not by reading one.

We decide two things. A `before_apply` entry may be a **Setup group**: a table
whose `parallel` array holds commands the author declares independent, which pop
then runs together. The entry list itself stays ordered, and a bare string stays
one command, so every Workbench on disk keeps its meaning. And every
human-authored command whose output pop *streams* — a Setup command, and the
project and worktree pickers' user-defined commands — runs in the **Human
shell**: the shell a tmux pane would get, resolved as tmux resolves it
(`default-shell`, then `$SHELL`, then `/bin/sh`) and started interactive, so the
human's functions and aliases resolve. A command whose stdout pop *parses* keeps
a bare `sh -c`, because an interactive shell prints its owner's banners into
what the parser is reading.

## Considered options

- **Let the shell own parallelism** — no pop change; the author writes
  `a & p1=$!; b & p2=$!; wait $p1 && wait $p2`. Rejected. It works, and it is
  what the file says today, but the honest spelling is unreasonable to expect:
  the obvious one (`a & b & wait`) reports success even when a job failed, and
  pop can never name which of the commands broke. A hook that swallows failures
  is worse than a hook that schedules.
- **An array of tables with a `run` list** (`[[workbenches.before_apply]]` +
  `run = [...]`). Rejected on a real misreading: every other array in pop's
  config is ordered, so an array nested inside an ordered list of tables reads
  as more sequence. The key has to say `parallel` for the shape to be unreadable
  as anything else. The mixed string-or-table array also follows the decoding
  pop already does for agent lists and topic steps.
- **Reinterpreting the existing flat array as parallel.** Rejected — it would
  silently parallelize the decrypt-then-pull setups this key was added for.
- **Reusing `internal/fanout.Map`** for the group. Rejected: its own package doc
  promises one pure read over independent inputs, and quietly widening that to
  side-effecting writes makes the document a lie for every later reader.
- **Nested groups, or a `needs = [...]` dependency graph**, to express two
  independent chains. Rejected — see the division of labour above; `&&` inside
  one command already says it.
- **Running each command in its own tmux pane** and gating the apply on them.
  Genuinely tempting, because the session already exists by then and the human
  would watch real output in real panes with no renderer written. Rejected: the
  apply would have to block on `tmux wait-for` and lift exit statuses out of
  panes, trading one clean error return for remote plumbing, and a pane that
  exits takes its output with it.

## Consequences

- A Setup group gets **no stdin**. Two commands cannot share one terminal, and a
  command that silently waits for input is a hang nobody can see. Setup that
  prompts — the passphrase ADR-0075 named — is therefore a group of one, which
  is the truth about it: prompting work is sequential.
- A group's output is **buffered per command** and flushed in declaration order
  when the group ends, the failing command first. Live interleaving produces
  lines that cannot be attributed to a command — and two commands that both draw
  with `\r`, as bundler and pnpm do, fight over the one line.
- Because the output is held back, a group in flight draws a **Setup progress
  line**: one refreshing line naming every command in the group and its elapsed
  state, degrading to plain start and finish lines where stdout is not a
  terminal. Buffering without it cannot be told apart from a hang. Both apply
  paths can draw it: the picker's create handler runs after the picker program
  has returned, so no `tea` program holds the terminal.
- Group boundaries are **barriers**, and that is the only structure groups have.
  The barrier reading is the trap — two chains that must not wait on each other
  are one group of two commands, not two groups.
- Progress is ephemeral on the create path: pop switches the client to the new
  session moments later, leaving the lines behind in the pane the picker ran in.
  A failure is not ephemeral — it aborts the apply before any attach, so it
  stays on screen, which is why a failed group needs no report document.
- The first failure in declaration order aborts the apply, after the group's
  siblings finish. A group is a unit; pop does not report half of one.
- The Human shell reads the human's configuration on every command, so a slow
  shell profile is now a cost of applying a Workbench. Inside a group that cost
  is paid concurrently, so a group of two loads it once in wall-clock terms.
- Amends ADR-0075, which declared the entry list ordered and said nothing about
  which shell interprets an entry. Its own boundary survives untouched: setup is
  side effects on disk and in services, never environment propagation into panes.
