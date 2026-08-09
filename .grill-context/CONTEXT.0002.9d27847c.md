---
fragment: 9d27847c
generation: 0002
branch: master
---

~ Claim owner
  Who holds a **Ticket claim**, and the thing whose liveness *is* the claim's:
  `pane:%<id>/<pane-pid>` when the command runs inside tmux, else `pid:<pid>`.
  No configuration and no login concept — an owner is only ever compared for
  equality, and probed for life. A claim is live exactly while its owner is: a
  pane pop can read that is not sitting at a bare shell, or a pid that answers
  `kill -0`. The pane's own pid rides in the owner string because tmux reuses
  pane ids across server restarts and there is no longer a TTL to unwedge a
  stale match. No tmux server means no live pane owners at all, which reopens
  every claim rather than holding them on a guess.
  was: Who holds a **Ticket claim**: the tmux pane id when the command runs
  inside tmux, else the pid. No configuration and no login concept — an owner is
  only ever compared for equality. A claim expires four hours after it was taken
  or last renewed (its owner re-claiming renews it); past that `pop map next` may
  steal it, and always reports the steal, since a dead grilling window would
  otherwise strand its ticket forever. The TTL is the only liveness policy
  available when the owner is a pane rather than a process.

~ Ticket claim
  One grilling window's hold on one Decision ticket, taken by `pop map next`
  (first frontier ticket, atomically picked and claimed) or `pop map claim
  <map-id> <NN>` (the override for when the human names a ticket). It is a
  `work_item_claims` row in pop.db keyed by the item's Work ref and nothing else
  — never a file state, because a claim belongs to a live window and a
  file-borne one outlives everything able to release it. The scan overlays live
  claims onto tickets, which is where the derived `claimed` status comes from.
  `next` exits nonzero on an empty frontier; two windows racing it get two
  different tickets, because the pick and the write share one transaction. It
  ends two ways and only two: **Ticket resolution** releases it, or its **Claim
  owner** dies and the next read stops counting it — there is no TTL, no expiry
  steal, and no release verb, so re-running a spawn verb is the whole recovery
  path for a session that died mid-grill.
  was: (as above, ending) **Ticket resolution** releases the claim; otherwise it
  expires on its TTL.

~ Grilling pane
  One Decision ticket's pane inside a **Map session**, tagged with the ticket id
  and titled after the ticket file's stem, running the interactive agent on the
  wayfinding skill in work mode. Every ticket agent is a pane in the session's
  single `map` window under a `tiled` layout, so one window shows the whole
  frontier in flight. Spawned by `pop map next` and by **Frontier fan-out**,
  neither of which moves the caller unless asked (`--focus`, and the uppercase
  dashboard keys). A pane whose agent is still alive is a jump target and is
  never sent work again (ADR-0158); an idle one (bare shell) is respawned — and
  that same predicate is what keeps its **Ticket claim** alive, so reclaiming a
  ticket and respawning its pane can never disagree about whether a session is
  still going. The other writes (`register`, `claim`, `resolve`,
  `out-of-scope`) run **in place** and spawn nothing: an agent resolving a
  ticket from a Task-set pane must not relocate its human.
  was: (as above, without the shared-predicate clause) A pane whose agent is
  still alive is a jump target and is never sent work again (ADR-0158); an idle
  one (bare shell) is respawned.
