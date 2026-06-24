---
fragment: 2f6af327
generation: 0006
branch: master
---

~ Topic
  A normalized lowercase kebab slug (≤5 words, e.g. `debugging-auth-middleware`) naming the subject of an Agentic pane. It is single-sourced as a per-pane tmux property, so any tmux surface — not just pop's dashboard — can display it, and it is reusable in custom tmux labels. pop derives it once per pane via Topic recipes and normalizes the result; a Note still outranks it in display. avoid: summarization, title, pane name, label, summary
  was: A short, agent-derived phrase describing the subject currently under discussion in an Agentic pane (e.g. "debugging auth middleware"). Distinct from a Note, which the user authors by hand: a Topic is machine-guessed and overwritten as the conversation moves. It is displayed, dimmed, in the parenthetical slot only when no Note is set, and lives for the pane's whole monitored lifetime — cleared only on retirement, never by unfollow.

+ Topic recipe
  A pop-curated invocation of an agent CLI (local or remote) that pop runs to derive a Topic. pop tries configured recipes in order and uses the first non-empty result, so a failed or rate-limited agent falls through to the next. pop owns the recipes, prompt, and output normalization but links no model SDK and holds no API keys — auth lives in the CLIs.
  avoid: topic command, topic model
  under: Pane status
