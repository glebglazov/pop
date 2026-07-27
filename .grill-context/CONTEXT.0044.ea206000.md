---
fragment: ea206000
generation: 0044
branch: master (grill: integrate package extraction)
---

+ Agent integration profile
  The per-agent record of how each **Integration component** is wired for one agent: its status-wiring install, removal and detection behaviour, its **Agent install path** roots for file-based components, and the legacy artifacts to prune. One profile per supported agent (claude, codex, cursor, pi, opencode); the profile is what makes a JSON-hook agent and a file-drop extension agent interchangeable to the rest of integrate. Distinct from an **Agent integration**, which is the wiring actually installed on a machine.
  avoid: agent adapter, agent support matrix, agent catalog
  under: Agent integrations
