---
fragment: C41D8B92
generation: 0003
branch: master
---

~ Effort model skip
  (as before, with the expiry rule and the read surfaces amended)
  The skipped model is recorded machine-globally, and how long the skip holds is
  its own policy rather than the preset cooldown's: the adapter's parsed reset
  instant, else one hour, else never for a `Permanent` recovery — and never more
  than **24 hours**, whatever reset the refusal claimed. The cap is what the
  cursor spent-allowance capture licenses: that refusal states a monthly
  billing-cycle date weeks out, while the probe that re-tests it costs about
  three seconds and exits before the model is engaged, so daily re-probing is
  close to free and buys back every stale claim — a top-up, a plan change, a
  lifted spend limit. The stated reset is kept beside the capped expiry rather
  than replaced by it, and both read surfaces name both whenever they disagree
  (`opus (skipped 24h0m (stated 26d0h))`), because a surface showing only the cap
  would misreport the refusal.
  was: The skipped model is recorded machine-globally, with the adapter's parsed
  reset instant as its expiry, else one hour, else never for a `Permanent`
  recovery.

~ Agent quota reporting / cursor
  cursor's spent-allowance refusal is a recognised shape as of 2026-08-09: one
  bare non-JSON line (`ActionRequiredError: You've hit your usage limit for
  <family> … reset when your monthly cycle ends on <M/D/YYYY>.`), exit 1, the
  model never engaged. It condemns the model, not the login — the same
  cursor-agent on the same account runs the tier's next entry — so it answers a
  `Model`-scoped, time-healing **Agent proceed verdict**, and cursor's quota
  reset is parseable rather than blind. It names the model family, never the
  ladder entry, so the skip is keyed by the entry pop pinned.
  was: cursor declares no parseable quota reset and its spent-allowance wire
  shape is unconfirmed.
