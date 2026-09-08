## Parent

`docs/adr/0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md`,
decisions 8, 9 and 10.

## What to build

The Work dashboard's derived row ordering is called a **Work lift** everywhere, and
"pin" is left to the two manual senses the same product already spells that way (the
Pinned action menu, and the monitor dashboard's Following).

A Work view preset grants the behaviour by declaring `lift = true`. A config someone
already wrote saying `pin = true` keeps working exactly as before — the old spelling is
a permanent, silent alias, warning about nothing — but `lift` is the only spelling pop
itself writes or documents.

Nothing about *when* rows lift changes in this slice. This is the vocabulary moving, and
it goes first so the passes built on top of it are born with the right name.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

The behaviour and its grant:

- `config/work_view_presets.go` — the `Pin` field and its `desc` tag, the key allowlist
  that decides which preset keys are recognised, the shipped `active` preset that is the
  only one declaring it, and the decode arm that rejects a non-bool. The alias belongs in
  that decode arm: accept either key, prefer `lift` when both appear.
- `work/snapshot.go` — `SnapshotOptions.PinPane` and the `pinAttributed` function that
  performs the lift. Note the function's own local variable is already called `lifted`.
- `work/kind.go` — `Container.Pinned`, roughly line 155.
- `dashboard/dashboard.go` — the `Pinned:` list option around line 1068, and
  `preset.Pin = active.Pin` around line 2803.
- `ui/list.go` — `renderPrefix` and the `Pinned` option it reads for the `▸` cell.

Tests carrying the word in their names or filenames: `dashboard/pane_pin_test.go`,
`dashboard/pane_pin_rebuild_test.go`, `dashboard/pane_bound_checkout_test.go`,
`dashboardshell/pane_pin_test.go`, `cmd/work_status_pin_test.go`, and in
`config/work_view_presets_test.go` the two named `...DeclaresPin` and
`...PresetPinDecodes...`. Rename the files along with the identifiers.

**Do not rename these — they are different words that survived a grep:**
`store/drains.go`'s `ExhaustedPinned` (the retired Pinned quota backoff),
`dashboard/mute_menu.go`'s "pinned first" (about date ordering),
`tasks/agent_*.go`'s "pinned model" (a hand-set `--model`), and
`integrate/grill_overlay_drift_test.go`'s upstream drift pin.

Verify with: `go build ./... && go vet ./... && go test ./...`

## Type

AFK

## Acceptance criteria

- [x] A preset declaring `lift = true` grants the behaviour, and the shipped `active`
      preset is the only shipped one that declares it
- [x] A preset declaring `pin = true` grants exactly the same behaviour, silently — no
      warning, no config finding, no deprecation notice
- [x] A preset declaring neither lifts nothing, and one declaring a non-bool for either
      spelling is a config finding that does not fail the load
- [x] No identifier, filename or comment in the tree calls the derived behaviour a pin,
      and the four unrelated "pin" senses listed in Orientation are untouched
- [x] `go build ./... && go vet ./... && go test ./...` all pass

## Blocked by

- None - can start immediately
