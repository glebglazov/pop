---
fragment: 7F19E5DD
generation: 0012
branch: master
---

+ Prompt template
  The embedded markdown document a domain package renders into an agent prompt.
  Holds every word pop says to an agent for that prompt; holds no prose-free
  logic beyond whole-section conditionals and row ranges. Lives beside the code
  that enforces its contract (`tasks/prompts/`, `routine/prompts/`), never in a
  shared prompt package, because a prompt is a concept of its work kind.
  avoid: prompt string, prompt asset, prompt file
  under: Unfiled (pending consolidation)

+ Prompt view
  The named Go struct one Prompt template renders against. It is the boundary of
  what that prompt is allowed to know: every filesystem read, git call, join,
  filter and truncation happens in the view builder, so the template holds no
  seams and no derivations. Optional single facts reach it as header lines, not
  as per-field conditionals.
  avoid: prompt data, template context, prompt model
  under: Unfiled (pending consolidation)

+ Disposition invariant
  The one rule every attended-agent prompt states: the assisting agent does not
  *effect* a disposition — no status transition, no verdict, no accept — while it
  may *draft* artifacts for the human to confirm, which is what lets Verify-fail
  assistance write a Remediation task and Assist edit the task manifest. Written
  once as a shared Prompt partial rather than restated per gate, where five
  divergent forbidden-lists had left each gate silent about a different mutation.
  avoid: advisory rule, agent restrictions, gate prohibition
  under: Unfiled (pending consolidation)
