# Acceptance list

Status: draft

1. A Work view preset that declares `lift = true` grants the Work dashboard's derived row ordering.
2. The shipped `active` preset is the only shipped preset that declares the grant.
3. A preset that declares `pin = true` grants exactly the same behaviour, and the load emits no warning, no config finding and no deprecation notice for it.
4. A preset that declares both spellings takes the value of `lift`.
5. A preset that declares neither spelling lifts nothing.
6. A non-bool value for either spelling becomes a config finding and does not fail the load.
7. No identifier, filename, comment or document in the tree calls the derived ordering a pin, including the test files that carried the word in their names.
8. The manual and unrelated senses of "pin" stay as they are: the Pinned action menu, the monitor dashboard's Following, the retired `ExhaustedPinned` quota backoff, the "pinned first" date ordering, the hand-set "pinned model", and the upstream drift pin.
9. A pane in a sibling worktree of a checkout that a Task set is bound to lifts that set, where before it lifted nothing.
10. A pane in a checkout that a set is bound to gets the same rows in the same leading order as before, and the repository pass does not contribute to that answer.
11. A pane that carries a Task-set tag gets the tag answer unchanged, whatever work the repository holds.
12. A repository's Task sets lift whether or not they carry a Worktree binding.
13. A pane standing outside any repository, or in a repository pop knows no work for, lifts nothing and reports nothing.
14. The repository pass concatenates the answers of every Work kind that has work in that repository instead of stopping at the first, asserted with two kinds answering.
15. The pane's repository is resolved once when the dashboard launches: a dashboard left open across many rebuilds forks git no more than it does at launch, asserted against a counting git seam.
16. `pop work status` lifts nothing and forks no git for attribution.
17. A row that the active preset hides is still not lifted, and the preset's own narrowing is unchanged.
18. A pane standing anywhere in a repository lifts that repository's live Maps, and a Map that is not live does not lift by locality.
19. A repository that holds both a Task set and a Map lifts both from one pane, with the Task set rows ahead of the Map rows.
20. A pane inside a Map's own Work session still gets the stamp answer, unchanged by the weaker repository pass.
21. A Routine never lifts by locality, and a comment records that the asymmetry is deliberate.
22. `go build ./...`, `go vet ./...`, `go test ./...` and `make test` all pass.
