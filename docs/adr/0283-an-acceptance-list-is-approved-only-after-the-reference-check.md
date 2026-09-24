---
status: accepted
relates: "amends the Acceptance-list approval of [ADR-0268](0268-the-eval-harness-compares-a-bare-agent-with-pop-on-spend-and-graded-quality.md) and follows [ADR-0282](0282-an-acceptance-list-holds-no-gate-item.md)"
---

# An Acceptance list is approved only after the Reference check

## Context

The Grader sees the Acceptance list and the diff, and not the spec. In the
`drain-rendering` Case, the spec limits Task result lines to a whole-set
drain, and six list items do not. The Grader applied them to single-task runs
and gave the two Arms 11/18 and 14/18. Read against the spec, both are about
16/18. The historical Reference would have failed the same items. The list
was wrong, and nothing showed it before Trials were graded against it.

## Decision

Before an Acceptance list is used, the same Grader grades the Case's Reference
diff against it, on the parent tree, with the same gates, as it grades a
Trial. This is the Reference check. Each item that the Reference fails is
fixed, or the human accepts it with a reason. The check is kept with the Case
and names the list's behaviours, the Grader and the Reference range. A change
to one of them makes it stale. A Trial is not run or graded against a list
whose check is missing, stale, not graded, or has a failure without an
accepted reason.

An item is fixed against the spec. The Reference only shows where an item is
ambiguous. When the spec and the Reference disagree, the spec wins, and the
item is kept with an accepted reason.

## Considered options

- **Grade both Arms' diffs in one Grader call.** Rejected: grades become
  relative to the other diff, the order and the contrast add bias, blinding
  is weaker, and an ambiguous item stays ambiguous.
- **A panel of Grader models.** Rejected as the fix: it reduces random noise,
  not a wrong item. Disagreement between judges is still useful as a signal
  that an item is ambiguous.

## Consequences

- The accepted reasons are kept in the check record and not in the list,
  because the Grader reads the list for every Trial.
- A reason belongs to an item's text. When a list edit changes an item's
  number, a new check keeps the reason. When the item's text changes, the
  reason is dropped.
- A Trial grade names the list it was graded with, and the Rollup counts
  grades made with an earlier list as stale.
- The Reference is Pop's final historical result. So without the rule that
  the spec wins, the check would move lists toward what that Pop run did.
