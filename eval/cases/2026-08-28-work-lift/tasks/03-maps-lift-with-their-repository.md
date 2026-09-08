## Parent

`docs/adr/0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md`,
decisions 2, 3 and 7.

## What to build

A live Map lifts for a shell standing in the repository it belongs to, beside that
repository's Task sets rather than instead of them.

A Map is Trunk-rooted and its only attribution today is the Work session stamp, which
fires solely inside `pop-map-<id>`. So an ordinary editor shell in the repository has
exactly the same blind spot for a Map that the previous slice just closed for Task sets:
you are standing in the work and the dashboard does not know it.

The pass and its merge already exist after the previous slice. What this slice adds is
the Map kind answering it, and the proof that the merge is real — a repository holding
both a Task set and a Map lifts both, in kind precedence order, where a first-hit pass
would have silently dropped the Maps.

Routines stay out, and that is a distinction rather than an oversight: a routine is
project-scoped with no container-level locality to narrow it, its pane is short-lived,
and page B has no equivalent of the preset narrowing that keeps this pass to a row or
two. A routine names a schedule; a Task set and a Map each name a definite piece of work
that lives in a definite repository. Leave the Routine kind answering the tag pass only,
and say why in a comment so the next reader does not "fix" it.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `wayfinder/workkind.go` — the Map's `work.Kind` adapter, which already implements the
  tag/stamp attribution seam; the repository seam goes beside it.
- A Map has no checkout of its own, so its repository comes from the Trunk it is rooted
  at — the same resolution `pop map open` and the Map session use. `wayfinder/session.go`
  and the Map-session parts of `wayfinder/` are where that resolution already lives;
  reuse it rather than re-deriving a trunk here.
- `work/attribute.go` — `kindsInPrecedence` is what puts Task sets ahead of Maps inside
  the merged answer.
- `routine/attribute.go` — leave it answering the tag pass only; this is where the
  comment explaining the asymmetry belongs.
- `work/conformance_test.go` — the per-kind conformance table is the natural place to
  pin which kinds answer which attribution seam.

Verify with:
`go build ./... && go vet ./work/... ./wayfinder/... ./routine/... ./dashboard/... && go test ./work/... ./wayfinder/... ./routine/... ./dashboard/...`
then `make test` before finishing.

## Type

AFK

## Acceptance criteria

- [x] A pane standing anywhere in a repository lifts that repository's live Maps
- [x] A repository holding both a Task set and a Map lifts both from one pane, with the
      Task set rows ahead of the Map rows
- [x] A pane inside a Map's own session still gets the stamp answer, unchanged — the
      stronger pass is not overtaken by the weaker one
- [x] A Routine never lifts by locality, and a comment records why that is deliberate
- [x] `make test` passes

## Blocked by

- 02-a-repository-pass-lifts-its-task-sets
