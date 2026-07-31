---
status: superseded by ADR-0107
---

# Queue dashboard switches all rows to two lines below a width threshold

> **Superseded by [ADR-0107](0107-queue-dashboard-two-line-rule-is-width-120-gated-by-pane-height.md):** the two-line trigger is now width below 120 columns, gated by whether the pane is tall enough to afford it — replacing this ADR's "width below 80 columns **or** any visible set id over 36 characters" rule. What ADR-0107 carries forward unchanged is the global, uniform row height (all rows share it so scroll and cursor math stay uniform) and the ADR-0079 List-boundary extension that makes it possible; only the trigger condition no longer describes any behaviour.
