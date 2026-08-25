---
fragment: 6FA40C8C
generation: 0035
branch: master
---

~ Quota assurance offset
  A fixed two-minute buffer added once to a derived reset instant — a relative
  countdown and an absolute epoch alike — so a retry lands just after the
  provider's edge rather than on it. Applied at derivation only: the
  **Agent quota recovery wait** deadline and the cooldown row it reads are the
  same instant, never two that differ by a second offset applied downstream.
  avoid: retry-after, cooldown grace, reset buffer, reset skew
  was: A fixed two-minute buffer added on top of a provider-stated relative reset window when deriving `PauseResetAt`, so **Agent fallback** and pinned-agent cooldown fire slightly after the provider's own countdown rather than on its exact edge.

+ Agent refusal signature
  The channel an **Agent adapter** reads to recognise a refusal, declared as a
  capability beside its quota-reset capability: the typed field its capture
  carries when it has one, the provider's prose when it has none. Structured
  first, so a reworded message degrades to prose matching rather than to no
  detection at all.
  avoid: quota marker, error string match, detection phrase
  under: Agent integrations

+ Quota window class
  Which allowance a refusal exhausted — claude's `five_hour`, `weekly` or Opus
  limit, kimi's period, billing cycle or monthly — read from the refusal's typed
  field or, failing that, from the marker prose that names it. It bounds how long
  the window can still run, which is what a **Guessed quota cooldown** has
  instead of an instant.
  avoid: rate limit type, quota tier, window size
  under: Tasks

+ Guessed quota cooldown
  A preset cooldown whose instant no provider stated, recorded as such rather
  than passed off as a reading. Its expiry is a ceiling — the latest its
  **Quota window class** can run, computed once at the first refusal and never
  re-derived from a later one — so a refusal on the edge can no longer push the
  deadline further out each time it is retried. It ends when an
  **Agent quota probe** succeeds, not when the ceiling passes.
  avoid: blind cooldown, fallback hour, invented reset
  under: Tasks

+ Agent quota probe
  A cheap invocation that asks an exhausted **Agent preset** whether it will run
  yet, made only against a **Guessed quota cooldown** and only after a refusal.
  It runs on the store-pure attempt path — no **Drain**, no **Captured run**, no
  attempt against the **Task retry cap** — and its answer is binary: refused
  advances the next probe, allowed deletes the cooldown row so every parked
  waiter resumes. One machine-global claim, a lease of about a minute, keeps
  parallel checkouts from each probing the same preset. It never asks how much
  allowance remains, which is what keeps it clear of **Agent quota reporting**.
  avoid: quota check, availability probe, quota polling
  under: Tasks

~ Agent quota recovery wait
  The poll loop an implement process enters after **Agent quota pause**: park the
  drain (`quota_paused` terminal, **Runtime execution lock** released per
  ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery
  coordinator** until a **Recovery turn** is granted, then `BeginDrain` and
  resume at the **Quota recovery resume point**. A wait on a provider's stated
  instant ends when that instant passes; a wait on a **Guessed quota cooldown**
  ends when an **Agent quota probe** succeeds. Applies to foreground and
  unattended drains alike — the pane shows the wait rather than exiting for human
  re-run. Pre-reset it prints a local-time countdown on the regular poll cadence;
  post-reset it prints the **Recovery block reason** on change plus a periodic
  heartbeat, never on the fast external-deregistration check. SIGINT deregisters
  the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the
  open task and partial checkout changes are preserved.
  avoid: in-process sleep, quota retry loop, blocking wait, --yes-only wait
  was: The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume at the **Quota recovery resume point**. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run. Pre-reset it prints a local-time countdown on the regular poll cadence; post-reset it prints the **Recovery block reason** on change plus a periodic heartbeat, never on the fast external-deregistration check. SIGINT deregisters the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the open task and partial checkout changes are preserved.

+ claude quota signal
  What gates **Agent quota detection** for the `claude` preset. The refusal is a
  terminal result carrying `api_error_status` 429 beside a `rate_limit_event`
  whose `rate_limit_info.status` is `rejected`; that pair is the reading, and the
  marker prose it accompanies — `You've hit your session limit`, `weekly limit`,
  `Opus limit` — is kept beneath it as a fallback and for the **Quota window
  class** each marker names.
  avoid: session limit string alone, 429 alone, is_api_error_message
  under: Tasks

+ claude quota reset
  claude states its reset as an epoch, not a sentence: `resetsAt` on the last
  `rate_limit_event` whose status is `rejected`. Only a rejection dates a pause —
  the same event reports `allowed` and `allowed_warning` throughout a healthy run
  — and the figure is padded by the **Quota assurance offset** and discarded past
  the cooldown horizon, since the recovery wait parks on it directly. The prose
  clause is the fallback for a capture carrying no such event: it reads an
  hour with or without minutes, in the zone the message names.
  avoid: resets clause parsing, utilization, allowed_warning reading
  under: Tasks
