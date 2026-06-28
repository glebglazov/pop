---
status: accepted
---

# Topic derivation rides core status wiring

A **Topic** — a short, agent-derived phrase describing what a pane's conversation is about — is reported into the **Monitor** by a dedicated `pop pane set-topic` command on its own `UserPromptSubmit` hook, installed automatically as part of **core status wiring** rather than as a separate opt-in **Integration component**. Running `pop integrate` therefore now implies consent to topic derivation as well as `set-status`, deliberately expanding the scope of the one implied-consent component. We chose this because the feature's whole value is being low-key: a user who must opt in, configure, and remember it will not get glanceable topics, whereas folding it into core makes dimmed topics simply appear on the next integration refresh.

The topic hook stays a *separate* hook entry and a *separate* command from `set-status` — `set-status` keeps its single responsibility (working/unread/clear) and the topic path is independently testable. "Core status wiring" is the consent unit they share, not the code path.

## Considered Options

- **New opt-in Integration component ("Topic reporting").** Rejected: matches the consent philosophy more purely, but adds a fourth component to the wizard and makes the default experience require a deliberate opt-in — defeating the low-key goal that motivated the feature.
- **Core wiring writes Topic but display is gated behind a config flag.** Rejected: splits the decision oddly (plumbing runs for everyone, output hidden by default) for no real benefit — most moving parts, worst mental model.
- **Fold topic-setting into the existing `set-status` invocation.** Rejected: bloats `set-status` with prompt-text handling and couples two unrelated reports; a separate hook keeps each command pure.

## Consequences

- Existing integrated agents begin emitting and displaying topics on the next integration refresh with no new consent step — a behavior change to a component previously promised to be minimal plumbing.
- Topic is a per-agent capability layered on status wiring: it only works where the agent's hook exposes prompt text, and degrades silently elsewhere. This is acceptable because `set-status` already varies per agent the same way.
- Demoting Topic to an opt-in component later would be a user-visible downgrade, so the implied-consent decision is effectively sticky.
