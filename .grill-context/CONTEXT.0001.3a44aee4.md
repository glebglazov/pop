---
fragment: 3a44aee4
generation: 0001
branch: master
---

+ Attended launch-time skip
  The one budget-awareness an attended session has: at launch pop takes the first
  entry of the attended agent list whose preset has no active quota cooldown and
  whose binary is on PATH, and names in the launch line what it skipped and why.
  Unlike **Agent fallback** it cannot switch mid-session — an attended agent's
  quota exhaustion is reported inside its own TUI, which pop never parses — so
  this is a pre-flight read of the cooldown rows drains write, not a fall-through.
  avoid: attended fallback, attended agent rotation
  under: Agents
