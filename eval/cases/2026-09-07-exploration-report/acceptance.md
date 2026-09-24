# Acceptance list

Status: approved

1. When a task set's folder holds `exploration.md`, `pop tasks artifacts` lists it as an artifact with its own type, "exploration". It comes after the spec and before the progress record.
2. The dashboard detail view lists `exploration.md` as an artifact with its own type, in the same position: after the spec, before the progress record.
3. A set folder that holds `exploration.md` with no manifest entry for it still registers. The orphan-markdown check ignores the file, as it ignores `spec.md`.
4. Registration still refuses every manifest and folder that it refused before, including other markdown files that no entry lists.
5. The manifest accepts a set-level `"explore": true`, and code can read the value beside the verifier's and the refiner's.
6. When `"explore"` is absent or false, the set never explores automatically. The key is opt-in, whereas `verify` and `refine` are opt-out.
7. The manifest accepts an `"explorer"` object with `agents` and `effort`, in the same shape as the verifier and refiner directives.
8. `pop tasks authoring-guide` documents the `explore` key and the `explorer` key. It says in as many words that `explore` is opt-in, while `verify` and `refine` are opt-out.
9. The authoring guide gives the rule for setting `explore`: two or more AFK tasks touch the same seam, and a later task depends on how an earlier task shapes that seam.
10. The authoring guide gets its new key names and file names from the same constants that the validator reads, and a test asserts that the two cannot drift.
11. A new command, `pop tasks explore <set-id>`, starts a fresh agent. Its prompt contains the set's manifest listing, every task body, and the spec when the set has one.
12. The explorer prompt asks for the code as found in the area that the tasks touch: where things live, which seam owns what, the invariants that hold, the test shape, and the files to read in ranges.
13. The explorer prompt tells the agent that every claim must name a path or a symbol, that the report must be about one page, and that the report must describe the tree it was observed at.
14. The explorer prompt asks for nothing about prior art, and the report has no prior-art section.
15. A successful explore pass writes `exploration.md` flat in the set directory, with no timestamped file or subdirectory. When a report already exists, the pass overwrites it.
16. The explore pass files a captured run under a phase of its own, and it spends against a retry cap of its own, beside implement, verify and refine.
17. A hand-run `pop tasks explore` runs on any set, whatever the set's `explore` key says, and it also runs when the machine config switches automatic exploration off.
18. A failed, timed-out or interrupted explore pass writes no report, and it leaves an existing report unchanged.
19. Tests drive the whole explore path together: the command, the agent reply, the report on disk and the captured run row, not each piece separately.
20. When a set has an exploration report, each implement attempt's prompt gives the report's path and says what the report is. When a set has no report, the prompt says nothing about exploration.
21. The implement prompt gives the report as a path and never puts the report's content in the prompt, whatever the report's length.
22. The implement prompt tells the builder to edit the report only to fix a line that this attempt made false, and to change nothing else in it.
23. The implement prompt's edit-boundary text names the report as an allowed exception beside the task file, so the boundary stays true.
24. The implement prompt's prior-art block ("Before you hand-write mechanism") is byte-for-byte unchanged.
25. An assist session's prompt gives the exploration report's path, beside the refine report pointer and the recent progress.
26. Golden prompt tests cover the implement and assist prompts for a set with a report and for a set without one.
27. A drain of a set that declares `explore` and has no report runs the explore pass first, before it selects any task.
28. A drain of a declared set that already has a report runs no explore pass and spends nothing on exploration.
29. A drain of a set that never declared `explore` runs no explore pass.
30. A config group of its own gates automatic exploration for the whole machine, and the gate is on by default. When the group or its key is absent, drains explore.
31. When that config switches exploration off, no drain runs an explore pass, but the hand-run `pop tasks explore` still works.
32. The manifest's `explorer` object overrides the configured agent list and effort for the explore pass, and CLI flags override the `explorer` object.
33. The drain's explore step does not run for a human-completed set, the same as refinement.
34. Nothing compares the tree recorded in the report to the checkout. When a report is absent, the drain writes one. When a report is present, the drain reuses it.
35. A test drives a whole drain of a declared set in two cases: with no report, where the pass runs, and with a report already there, where no pass runs. The test asserts what ran in each case.
36. When the explore pass fails, it retries up to a standard per-phase cap before it gives up.
37. A declared set whose explore pass gave up and wrote no report shows a park status of its own (for example EXPLORE-FAILED), and the drain stops at that status.
38. The park status comes from the explore phase's captured run plus the absence of the report. It adds no store column and no table.
39. The park status is different from BLOCKED, and BLOCKED keeps its meaning that a human owes a decision.
40. Automatic selection and the Work daemon skip a parked set, so nothing explores it again in a loop.
41. When a set parks, exactly one set-level progress record is added. It names the explore phase and the reason.
42. The implement drain has a skip flag. With it, a declared set drains once without exploring and without parking.
43. A successful hand-run explore pass clears the park, and removing the `explore` declaration from the manifest also clears it.
44. The status-derivation text in `CONTEXT.md` includes the new park status in the list of statuses where draining stops. It also lists it among the statuses that automatic selection skips.
45. A set that declared `explore` and has no report shows an exploration mark beside its status. The reason (not yet run, or the pass failed) appears as detail beside the mark, not inside the mark's value.
46. A set that never declared `explore` shows no exploration mark on any surface.
47. The exploration mark is resolved once on the read side, beside the verification mark, and the dashboard row, the dashboard detail view, `pop work status` and `pop tasks status` all show that one result.
48. The dashboard detail view shows the exploration report as a pointer (its path plus the tree it records) and never shows the report's body.
49. The sign-off gate names the exploration report in the same place where it names the refine report.
