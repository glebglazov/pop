---
fragment: 5745C75C
generation: 0044
branch: master
---

~ Fast-pass decision
  A call **grill-with-docs-fast** makes instead of asking, because the answer is
  findable or does not reshape the rest of the design tree. Bounded to *who answers a
  frontier question* and never to whether the work starts: implementing the change under
  discussion is never a fast-pass decision, however settled the session judges the
  design to be. Reported to the human in the round's ledger — the call, the rejected
  alternative, one clause of why — and persisted in the session's closing commit body,
  never in the glossary or an ADR.
  avoid: auto-decision, assumed answer, silent default
  was: A call **grill-with-docs-fast** makes instead of asking, because the answer is findable or does not reshape the rest of the design tree. Reported to the human in the round's ledger — the call, the rejected alternative, one clause of why — and persisted in the session's closing commit body, never in the glossary or an ADR.
  under: Language

+ Hand-off ask
  The question that ends both grilling composers' close, after the commit and never
  before it: implement the settled plan now in this session, or leave it for a separate
  step (`to-tasks`). The answer is the human's licence to start work — a session infers
  it from no amount of settledness — and whichever option they name, this session
  carries out (ADR-0257).
  avoid: next-step prompt, wrap-up question, implementation offer
  under: Language

~ grill-with-docs-fast
  The human-opened fast-pass sibling of **grill-with-docs**: the same contract and the
  same close, conducted in as few rounds as possible by deciding the non-critical calls
  itself and reporting them as **Fast-pass decision**s. Its override of the interview
  primitive reaches only who answers a frontier question; the work itself waits for the
  **Hand-off ask**, and its body carries the clause saying so because it is the only
  body that negates the wait-sentence.
  avoid: fp-grill-with-docs, fast grill, quick grill
  was: The human-opened fast-pass sibling of **grill-with-docs**: the same contract and the same close, conducted in as few rounds as possible by deciding the non-critical calls itself and reporting them as **Fast-pass decision**s.
  under: Language

~ grill-with-docs
  The human-opened standalone grilling workflow: composes the Agent-loaded `grilling`
  and `domain-modeling` skills, and owns the **Shared skill document** `GRILL-SESSION.md`
  that carries Pop's once-per-round glossary timing, unified fact-finding and close —
  commit, then the **Hand-off ask** — for itself and **grill-with-docs-fast** alike.
  Never loaded by a wayfinding ticket because its contract writes and commits repository
  artifacts.
  avoid: grill-me, the grilling skill
  was: The human-opened standalone grilling workflow: composes the Agent-loaded `grilling` and `domain-modeling` skills, and owns the **Shared skill document** `GRILL-SESSION.md` that carries Pop's once-per-round glossary timing, unified fact-finding and commit-on-close rules for itself and **grill-with-docs-fast** alike. Never loaded by a wayfinding ticket because its contract writes and commits repository artifacts.
  under: Language
