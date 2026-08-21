# A drain stops only when it has run out of agents

A drain may end for exactly three reasons: the work finished, the human
interrupted it, or the **Agent fallback** walk reached the end of its list.
Nothing else ends one — not a failed **Task attempt**, not a spent **Task retry
cap**, not one agent refusing to run — because each of those still leaves an
untried agent in the list, and stopping in front of an untried agent is the
failure this decision exists to prevent.

## Context

A task set lost a whole night to this. Codex was the second entry in
`[work.implement].agents = ["claude", "codex", "kimi"]`. It hit an OpenAI
workspace spend cap, exited 1 in eight seconds, three times. Pop marked the task
Failed and stopped the drain. Kimi — healthy, authenticated, third in the list —
was never invoked. The human reopened the task by hand the next morning and
claude finished it in sixteen minutes.

Three separate defects lined up to produce that:

1. `codexQuotaPauseReason` anchored on the literal `"You've hit your usage
   limit"`. The spend-cap wording is different, so no **Agent proceed verdict**
   was produced.
2. `assessAttempt` short-circuits on a non-zero exit and returns `"agent exited
   with status %d"` without reading the agent's output — discarding the
   provider's own sentence, which the codex adapter had already parsed.
3. With no verdict, `executeTaskAttemptsWithAgentFallback` returned outright
   rather than advancing. Only a verdict-shaped stop ever advanced the walk.

Defect 3 was known. CONTEXT recorded it as *"Verify's list falls through on one
more class — a preset's exhausted retry loop — an asymmetry with implement that
is deliberate and unresolved."* This ADR resolves it in verify's favour.

## What the evidence said

Every failed run captured on the machine was examined: 49 non-zero-exit
failures across two months, and every single one was the provider falling over.
Not one was the task's fault.

| exact message | count |
| --- | --- |
| `API Error: Connection closed mid-response` | 21 |
| codex spend cap | 10 |
| `API Error: Your computer went to sleep mid-response` | 6 |
| `API Error: Unable to connect to API (ENOTFOUND)` | 6 |
| `API Error: 529 Overloaded` | 5 |
| `API Error: Unable to connect to API (ConnectionRefused)` | 1 |

Non-zero has only ever been `1`; the code itself carries no information. Task
faults exist in the corpus but live entirely on the exit-zero side — unchecked
acceptance criteria, a missing `TASK_COMPLETE` sentinel, an agent reporting a
real blocker it found. That split is what the disposition rule below rests on.

Thirty-four of the 49 are the laptop rather than any provider: sleeping or
losing its network mid-run, overnight, after the agent had already done real
work. Pop cannot prevent that. It can decline to charge it to the task.

## Decision

**Retry-cap exhaustion advances the walk.** A preset that spends its whole cap
without finishing hands the turn to the next preset, exactly as a preset-scoped
verdict does. Every **Work group** behaves this way; the implement/verify
asymmetry is gone. The timeout branch advances too — it returned out of the walk
by the same mistake.

**Disposition follows the exit code, once the walk is exhausted.** A non-zero
exit leaves the task **Open**, so **Work supervision** retries it later; an
exit-zero failure whose contract was unmet leaves it **Failed** for a human. A
**Task attempt timeout** that survives every agent is **Failed**: nine attempts'
evidence that the work does not fit in one attempt is not something tomorrow
fixes, and what it needs is the task split.

**A full stop is per-drain, not per-task.** A set that cannot get an agent for
one task will not get one for the next, and grinding every remaining task
through the same dead list pays for the same discovery many times.

**A walk in which nothing could start is a no-op.** Every preset cooling,
capped, unauthenticated or absent from PATH means nothing was attempted, so
nothing failed and no task changes state. It ends the drain, but it is a
different event from exhaustion and must not read as failure.

**An `Agent spend cap` is a first-class verdict flavour, cooling for one hour.**
Pop supplies the hour itself — this is the only cooldown pop invents rather than
reads out of a provider's message — so a cap enters the ordinary **Agent quota
recovery wait** and the preset rejoins the walk when the hour is up. Hitting it
again starts another hour.

**The journal ranks what cost a drain its agents.** A **Work journal** entry
carries a severity, and the two endings above — a spent agent list and a walk
that started nothing — are severe, recorded as a **Drain ending** on the drain
row because both stop on an ordinary clean-finish exit reason and would
otherwise be filed beside a healthy drain. A healthy fall-through is not severe:
one agent stepping aside for one that finishes the work is pop working, and it
must not compete for attention with a drain that lost every agent it had.
`pop work log --severe` is the whole answer to "what went wrong while I was
away?", over the last day by default, each entry naming its task set and agent.

**The provider's diagnostic survives.** `assessAttempt` reads the adapter's
parsed output on a non-zero exit instead of discarding it. This is the smallest
change here and the highest-leverage: it is what makes the **Progress record**,
the **Work journal**, and the prior-attempt digest handed to the *next* agent
say `Your computer went to sleep mid-response` rather than `status 1`.

## Considered options

**An agent classifier deciding provider-fault versus task-fault.** Prototyped
against the real corpus with the local `gemma4:e4b` already configured for pane
topics. It was accurate where it mattered — correct on the spend cap and on
`Connection closed mid-response`, honestly UNCLEAR where the tail genuinely did
not say. It was rejected anyway, because the cases it would have to adjudicate
are the ones pop already answers deterministically: an exit-zero contract
failure needs no model, and the exit-code rule got all 49 non-zero cases right
on its own. A classifier costs a run, needs an agent that is up in order to
explain why agents are down, and adds a failure mode to the machinery whose
entire purpose is not failing. Revisit it if a non-zero exit ever turns out to
be the task's fault.

**Pre-flight spend detection, and a setting for whether pop may spend into a
cap.** Rejected as unbuildable rather than undesirable. No agent CLI reports a
remaining allowance — `codex doctor` covers auth and reachability and nothing
else — so the only figures available arrive mid-run on a `token_count` event.
A reading remembered from an earlier run goes stale silently when a subscription
resets, and unlike a quota pause there is no reset instant to expire it against.
A setting resting on an unreliable reading is worse than no setting.

**Treating a spend cap as human-healing, like an authentication failure.** It is
the honest classification: only the workspace owner can lift it, and polling
cannot. Rejected because the honest classification produces the worse outcome —
the drain exits and nothing resumes until a human notices, which is the same
discouragement as the original bug wearing different clothes. The invented hour
is a deliberate lie that self-corrects: a cap raised over lunch costs nothing,
and one nobody ever raises costs a single refused eight-second invocation per
hour.

## Consequences

The empty cell in pop's test matrix is now load-bearing and must be filled.
Every existing test that emits real provider prose exits zero
(`installClaudeQuotaAgent` and its family); every test that exits non-zero emits
no prose (`TestRunTaskSetFailedTaskStopsDrain`). The two axes are never crossed,
which is exactly why all three defects above survived a well-tested package.
Neither test double can express the missing case: `fakeAgentConfig` carries an
`exitCode` but only ever prints a well-formed summary block, and `attemptScript`
has no exit code at all. Both need a `rawOutput`, and `attemptScript` needs an
`exitCode`, so that "attempt 1 prints this provider text and exits 1" becomes
writable. No state machine is required — the drain loop is already phase-based —
and the captured streams stay a corpus to derive fixtures from rather than
something committed.

A machine-global per-preset cooldown means one drain discovering a spend cap
protects every other set on the machine, across implement, verify and review
alike, for the hour.
