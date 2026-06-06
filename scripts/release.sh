#!/usr/bin/env bash
# Cut the next CalVer release tag (ADR-0002): vYYYY.M.R — current UTC month,
# zero-based counter that resets monthly. Runnable locally or from CI; the
# Release workflow picks the pushed tag up and publishes the binaries.
#
# Refuses to tag when nothing changed since the last release. Writes
# should_release/tag to $GITHUB_OUTPUT when running inside Actions.
set -euo pipefail

calver='^v[0-9]{4}\.[0-9]{1,2}\.[0-9]+$'

# Local runs may have stale tags; CI checks out with fetch-tags.
if [ -z "${GITHUB_ACTIONS:-}" ]; then
  git fetch --tags --quiet
fi

# Most recent existing release tag, version-sorted so .10 beats .2.
prev="$(git tag --list --sort=-v:refname | grep -E "$calver" | head -n1 || true)"

if [ -n "$prev" ] && git diff --quiet "$prev" HEAD; then
  echo "No changes since $prev — nothing to release." >&2
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "should_release=false" >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi
echo "Previous release: ${prev:-<none>}" >&2

# Non-zero-padded month (ADR-0002): v2026.6.R, not v2026.06.R.
base="v$(date -u +%Y).$(date -u +%-m)"

# Next zero-based release counter for the current month.
next=0
while IFS= read -r t; do
  [ -z "$t" ] && continue
  r="${t##*.}"
  if [ "$r" -ge "$next" ]; then next=$((r + 1)); fi
done <<< "$(git tag --list "$base.*")"
tag="$base.$next"

git tag -a "$tag" -m "Release $tag"
git push origin "$tag"
echo "Released $tag" >&2

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "should_release=true" >> "$GITHUB_OUTPUT"
  echo "tag=$tag" >> "$GITHUB_OUTPUT"
fi
