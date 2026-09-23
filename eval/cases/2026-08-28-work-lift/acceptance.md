# Acceptance list

Status: approved

1. A Work view preset that declares `lift = true` grants the Work dashboard's derived row ordering.
2. The shipped `active` preset is the only shipped preset that declares the grant.
3. A preset that declares `pin = true` grants exactly the same behaviour, and the load emits no warning, no config finding and no deprecation notice for it.
4. A preset that declares both spellings takes the value of `lift`.
5. A preset that declares neither spelling lifts nothing.
6. A non-bool value for either spelling becomes a config finding and does not fail the load.
7. No identifier, filename or comment in the tree calls the derived ordering a pin, including the test files that carried the word in their names. ADRs are historical records and are outside this item.
8. The manual and unrelated senses of "pin" stay as they are: the Pinned action menu, the monitor dashboard's Following, the retired `ExhaustedPinned` quota backoff, the "pinned first" date ordering, the hand-set "pinned model", and the upstream drift pin.
9. The preset field's description and every preset pop ships spell the grant `lift`; pop reads `pin` only as an alias.
10. A pane in a sibling worktree of a checkout that a Task set is bound to lifts that set, where before it lifted nothing.
11. A pane in a checkout that a set is bound to gets the same rows in the same leading order as before, and the repository pass does not contribute to that answer.
12. A pane that carries a Task-set tag gets the tag answer unchanged, whatever work the repository holds.
13. A repository's Task sets lift whether or not they carry a Worktree binding.
14. The repository pass matches a pane to work by the repository's git common directory, not by project name, so a sibling worktree matches and two repositories that share a project name do not share work.
15. A pane standing outside any repository, or in a repository pop knows no work for, lifts nothing and reports nothing.
16. The repository pass concatenates the answers of every Work kind that has work in that repository instead of stopping at the first, asserted with two kinds answering.
17. The pane's repository is resolved once when the dashboard launches: a dashboard left open across many rebuilds forks git no more than it does at launch, asserted against a counting git seam.
18. `pop work status` lifts nothing and forks no git for attribution.
19. A row that the active preset hides is still not lifted, and the preset's own narrowing is unchanged.
20. A pane standing anywhere in a repository lifts that repository's live Maps, and a Map that is not live does not lift by locality.
21. A repository that holds both a Task set and a Map lifts both from one pane, with the Task set rows ahead of the Map rows.
22. A pane inside a Map's own Work session still gets the stamp answer, unchanged by the weaker repository pass.
23. A Routine never lifts by locality, and a comment records that the asymmetry is deliberate.
