---
status: superseded by ADR-0083
---

# Config resolution is driven by a leaf-keyed settings registry

> **Superseded by [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md):** The design narrowed to a curated handful of genuinely repo-specific keys (`workbenches`, `preferred_workbench`) authored through one schema shared by `.pop.toml` and `[repo."<path>"]` (ADR-0083). That does not warrant a general leaf-keyed settings registry, a merge-strategy engine, or `resolveVia` — the original body is removed so a reader does not build machinery the project decided against.
