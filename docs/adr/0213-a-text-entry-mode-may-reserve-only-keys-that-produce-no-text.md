---
status: accepted
---

# A text entry mode may reserve only keys that produce no text

## Context

[ADR-0081](0081-house-text-field-consolidates-single-line-input.md) made
`ui.TextField` the one house single-line editor and left the routing question to
each host: *"callers intercept their own reserved keys first and forward only the
remainder to the field."* That sentence is a permission with no ceiling, and five
hosts each took a different amount — `ui/picker.go`, `ui/nameprompt.go`,
`ui/configdashboard.go`, `ui/configurepicker.go`, and the Work dashboard's `/`
query.

The Work dashboard is where the permission failed. `updateFilterMode`
(`dashboard/dashboard.go:1815-1837`) reserved `j`, `k` and the arrow keys for row
navigation and let the shell keep `v` for the page toggle, so while a query was
open:

- No project or set whose name contains `j`, `k` or `v` could be typed. `jj` and
  `k8s` are real names.
- No action key worked. Enter fell through to the field, which has no binding for
  it, so it was a silent no-op — you could narrow to the row you wanted and then
  not open it. The only exit was Esc, which also discarded the query, re-expanded
  the list and moved the cursor.
- `v` switched pages out from under a half-typed query, because
  `ViewToggleAllowed()` (`dashboard.go:776`) never checked the mode.

The mode had no end. Typing *was* the state, so every key had to be either a
character or a navigation key forever, and the surface resolved that by stealing
letters. The operator-visible symptom was "I can't use j/k with a filter open" —
the inverse of the truth, which is that j/k were the *only* thing that still
worked.

Two hosts also can't be fixed by the same shape. The Work dashboard's query can
have a committed phase — apply it, get the whole keymap back. An incremental
picker cannot: you type and move the selection simultaneously and Enter picks, so
it must reserve navigation keys or it cannot function. A rule stated as a fixed
list of three keys would have made the pickers illegal.

## Decision

A **Text entry mode** — any TUI mode whose keyboard belongs to a `ui.TextField` —
may reserve **only keys that produce no text**: arrows, `tab`, `pgup`/`pgdn`,
`ctrl+n`/`ctrl+p`, `ctrl+c`, and its own commit and cancel keys. Every printable
key is text, always. No host may reserve a letter or a digit.

This is a **ceiling, not a quota**: each host reserves the least it can, and the
difference between hosts is then explained by their designs rather than by
improvisation. The Work dashboard search reserves only Enter, Esc and `ctrl+c`,
because a committed search has no reason to navigate mid-type. An incremental
picker reserves arrows and `ctrl+n`/`ctrl+p` as well, because selection movement
is inseparable from its typing.

`ctrl+c` stays reserved everywhere as quit. It is not a text key in any editor and
"ctrl+c gets me out" is a terminal-wide contract a single mode should not break.

Word-level editing belongs to the shared field, not to hosts:
`ui.TextField`'s keymap gains `ctrl+w` (delete word back), `alt+b`/`alt+f` (word
jump), `alt+d` (delete word forward) and `ctrl+k` (kill to end), alongside the
existing arrows, `ctrl+b`/`ctrl+f`, `ctrl+a`/`ctrl+e`, backspace and `ctrl+u`.
All five hosts gain them at once, and none of them collide with the reservable
set.

This amends ADR-0081's routing sentence from a permission into a constraint. The
`ctrl+a`→home reasoning there is unaffected: a host's `ctrl+a` binding outside a
text entry mode is not covered by this rule.

The first application is the Work dashboard's `/`, which becomes the **Work
dashboard search** — a committed query with a typing phase and a navigating
phase — so that "reserve nothing printable" is a design it can actually meet. The
other four hosts are audited in the same change and reported, not swept: a
picker's reserved keys are load-bearing for its own navigation, and each
divergence is its own conversation. Trivially compliant hosts are fixed in
passing.

## Considered options

- **Reserve only commit, cancel and `ctrl+c`.** The rule as first stated. Gets the
  dashboard exactly right and makes `ui/picker.go` and `ui/configurepicker.go`
  unimplementable — they would have no way to move a selection. Rejected: a rule
  that outlaws two existing correct surfaces is describing one surface, not a
  house standard.
- **Leave routing to each host, fix `/` alone.** Smallest change. Rejected: the
  divergence is the defect. Five hosts improvising five reserved sets is what
  produced an unsearchable `j`, and nothing stops the sixth host from repeating
  it.
- **Let the field claim every key and give hosts an escape hatch flag.** Inverts
  the default so hosts opt *out*. Rejected: the same improvisation with a wordier
  API, and it gives no principle for deciding which opt-outs are legitimate.

## Consequences

- The glossary gains **Text entry mode**, and **Work dashboard search** as the
  first surface built to it; **Work dashboard filter menu** is redefined so
  "filter" names only the preset menu and "search" only `/`. The chrome follows:
  the vocabulary collision between two subtractive keys was half the reported
  confusion.
- The `/` mode's inert-action-key behaviour is now a bug rather than a design.
  The tests pinning it invert — `TestDashboardFilterMode_NavigationWorksInsideFilter`
  and `TestDashboardFilterMode_BareActionsInertInFilterMode`
  (`dashboard/dashboard_test.go:3102`, `:3127`) assert the opposite of the new
  rule.
- `ViewToggleAllowed()` must account for the typing phase, so `v` is typeable.
- A future host that wants a letter binding while text is open has to change this
  ADR rather than its own switch statement, which is the point.
