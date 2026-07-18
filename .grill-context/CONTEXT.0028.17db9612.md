---
fragment: 17db9612
generation: 0028
branch: master
---

+ Dashboard verify verb
  The `v` verb in the Queue dashboard action menu that spawns a pane running
  `pop tasks verify <set> --task-runtime-path <checkout>` on a NEEDS-VERIFY or
  VERIFY-FAILED set — a manual, un-locked Verifier force that reuses the drain
  pane-per-set tagging but records no Runtime execution lock or spawn intent, and
  is hidden on live-drain rows because a plain verifier run is not
  quiescence-gated.
  avoid: re-verify button, verify key, verify action
  under: (dashboard terms — near Live-drain indicator)
