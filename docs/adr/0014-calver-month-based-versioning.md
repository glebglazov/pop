# CalVer month-based versioning, no stability contract

Status: accepted

Pop gains versioning for three consumers: identifying a beta tester's binary in bug reports, driving release automation, and the picker Update notice. None of them needs a compatibility signal — alias removal is already gated on beta-tester sign-off (CLEANUP.md), not a version number — so the version is deliberately just a date.

Decision: **CalVer in the mise style**. A release tag is `vYYYY.M.N` — year, non-zero-padded month, and a zero-based release counter that resets each month (`v2026.6.0`, then `v2026.6.1`; first July release is `v2026.7.0`). Every tag has exactly this 3-part shape. The version carries **no backward-compatibility guarantee and no breaking-change signal**; breaking changes are communicated through CLI deprecation warnings and beta-tester sign-off, mirroring how mise pairs CalVer with warning windows instead of major bumps.

A **Release** is: `scripts/release.sh` computes and pushes the next tag (runnable locally, or via the Release workflow's `workflow_dispatch`, which runs the same script and continues into publishing in the same job because `GITHUB_TOKEN`-pushed tags don't retrigger workflows); goreleaser then builds darwin/linux × arm64/amd64 binaries, publishes the GitHub release, and bumps the `glebglazov/homebrew-tap` formula from a HEAD source build to versioned binaries. `pop --version` reports `git describe --tags --always --dirty` (v prefix stripped): the bare version on a tagged build, `2026.6.0-5-gabc123` between releases.

The scheme, the script, and the workflow shape are deliberately identical to tdg-cli (its ADR-0002); divergence between the two repos' release plumbing is a smell.

## Considered alternatives

- **SemVer.** Rejected: its value is the major-bump breaking-change contract, and pop deliberately has no stability promise to encode — sign-off gates removals, not versions. Carrying semver would imply a guarantee that won't be honored.
- **Date-based CalVer (`YYYY.MM.DD(.PATCH)`).** Rejected: the day carries little meaning (the release date lives on the GitHub release), and the tag shape varies — 3 parts normally, 4 on a same-day re-release. A monthly counter keeps a uniform shape.
- **Zero-padded month (`v2026.06.0`).** Rejected for mise-exactness; padding buys nothing once the v-prefixed 3-part shape is chosen.
- **No `v` prefix.** Rejected: CalVer years break Go semantic import versioning either way (a major ≥ 2 must appear in the module path), so the prefix is cosmetic — and the conventional form matches mise, tdg-cli, and goreleaser defaults.
- **Tag-only releases, testers build from source.** Rejected: the tap already exists and prebuilt binaries drop the Go-toolchain requirement for testers.

## Consequences

- **`go install <module>@<version>` stays dead.** Go's toolchain rejects `v2026.x` tags for a module without a `/v2026` path suffix; `@latest` resolves via a commit pseudo-version. Acceptable: distribution is brew or release binaries.
- **A reader may misread the tag as semver** (`v2026.6.1` looks like major.minor.patch). Accepted: the 4-digit "major" reads as a year on inspection, and this ADR records that no contract exists.
- **Releasing requires a tap PAT** (`TAP_GITHUB_TOKEN` secret): the workflow token cannot push the formula bump to the tap repository.
- **Brew installs skip `pop integrate --update-existing`** (which `make install` runs); the runtime integration refresh on binary change covers it.
- **The Update notice depends on this scheme**: it renders only when the running version is exactly a release tag and a newer one exists; tag-relative dev builds suppress it (see CONTEXT.md "Releases").
- **Adopting a stability contract later is a deliberate, documented reversal** — a future ADR superseding this one.
