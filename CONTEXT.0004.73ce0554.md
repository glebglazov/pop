---
fragment: 73ce0554
generation: 0004
branch: tui-deepening-list-module
---

+ List
  The shared, generic scrolling-list viewport the pickers and dashboards stand
  on: it owns cursor, scroll, height, navigation, identity-preserving reload,
  and per-row drawing (the █ cursor block, quick-access prefix, padding),
  exposing the visible rows as strings for the caller to compose. A passive
  state+render module driven by the model (no key handling of its own); rows are
  generic with Key/Cell closures.
  avoid: list widget, viewport, scroller, picker (the picker is a List adapter)
  under: Pickers
