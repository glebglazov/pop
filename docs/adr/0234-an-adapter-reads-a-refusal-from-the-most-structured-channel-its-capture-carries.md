# An adapter reads a refusal from the most structured channel its capture carries

Every one of pop's six adapters recognises a quota refusal by matching English
prose, and two of them are matching prose that sits beside a typed field saying
the same thing. A provider that rewords its message does not merely mis-date the
pause it causes — pop stops recognising the refusal at all, which is a worse
failure than the one
[ADR-0233](0233-claude-dates-a-quota-pause-from-the-epoch-its-stream-states.md)
was written to fix.

## Context

ADR-0233 taught claude to date a pause from the `resetsAt` epoch on its own
`rate_limit_event`, on the reasoning that the wire figure is the same fact
without the two ways the sentence loses it. It changed only the dating.
Detection stayed where it was:

    for _, marker := range []string{
        "You've hit your session limit",
        "You've hit your weekly limit",
        "You've hit your Opus limit",
    }

So the structured field is read only once a prose match has already decided a
refusal happened. If the marker changes, the epoch beside it is never consulted,
and the whole reset-aware mechanism the last two ADRs built is bypassed by a
copy-edit.

codex is the same shape one layer further out. Its refusals arrive inside typed
`error` and `turn.failed` events, but the classification inside them is
`strings.Contains(strings.ToLower(message), "spend cap")` — two words carrying
the entire **Agent spend cap** verdict, while the preceding `token_count` event
carries `rate_limit_reached_type: "workspace_member_usage_limit_reached"`
unread. ADR-0231 recorded that field's existence and did not read it.

The remaining four are prose because prose is all they have: cursor's refusals
are bare non-JSON stderr lines, kimi writes quota diagnostics to stderr and never
to its stream, and opencode and pi share a matcher over the raw capture. That is
not an oversight to be fixed later, and nothing here should make it look like
one.

## Decision

**An adapter declares an Agent refusal signature, and reads the most structured
channel its capture actually carries.**

1. **Structured first, where structure exists.** claude reads
   `api_error_status: 429` together with a `rate_limit_event` whose
   `rate_limit_info.status` is `rejected`; codex reads
   `rate_limit_reached_type`. The pair is the reading — a 429 alone is a
   transient overload, and a rejection alone can be reported on an event pop is
   not otherwise acting on.
2. **The prose stays, beneath.** Three string comparisons are the only thing
   still working if a provider changes its event schema rather than its wording,
   and belt-and-braces here costs nothing. The markers are demoted, never
   deleted.
3. **A refusal names its Quota window class.** Both channels carry it —
   `rateLimitType: "five_hour"` structurally, and each marker sentence names its
   own limit in prose. This is why the demoted markers keep earning their line:
   they are the class reading for a capture whose typed field is absent.
4. **An adapter with no structured channel says so.** The signature is a
   capability beside `AgentQuotaResetCapability`, and takes the same `Blind`
   kind with a required reason — "cursor writes refusals as bare stderr lines",
   "kimi writes quota diagnostics to stderr, never to its stream-json". An
   absence that is stated cannot be read later as an unfinished job.

## Consequences

A reworded marker now costs pop nothing on claude and codex, and a changed event
schema costs it no more than it costs today. The capability makes the four
prose-only adapters legible as a deliberate limit of their providers rather than
as work nobody got to.

It does not reopen the deferred **Agent quota reporting** concern. The fields
read here say *that* a request was refused, never how much allowance is left;
`utilization`, which sits on the very same claude events, stays unread — the
same line ADR-0233 drew.
