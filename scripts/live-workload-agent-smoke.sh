#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/live-workload-agent-smoke.sh AGENT [AGENT...]
  POP_LIVE_AGENTS="codex claude" scripts/live-workload-agent-smoke.sh

Runs real workload execution against real agent CLIs in disposable git repos.
This consumes local agent auth/quota and is intentionally opt-in.

Environment:
  POP_LIVE_POP_BIN=/path/to/pop     Use an existing pop binary instead of building one.
  POP_LIVE_TIMEOUT=10m              Per-attempt timeout. Default: 10m.
  POP_LIVE_MAX_TRIES=1              Attempts per issue. Default: 1.
  POP_LIVE_KEEP=1                   Keep temporary repos after the run.
USAGE
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

agents=("$@")
if [[ ${#agents[@]} -eq 0 && -n "${POP_LIVE_AGENTS:-}" ]]; then
  # shellcheck disable=SC2206
  agents=(${POP_LIVE_AGENTS})
fi
if [[ ${#agents[@]} -eq 0 ]]; then
  usage >&2
  exit 64
fi

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/pop-live-workload.XXXXXX")"
if [[ "${POP_LIVE_KEEP:-}" != "1" ]]; then
  trap 'rm -rf "$tmp_root"' EXIT
else
  printf 'Keeping temporary live smoke root: %s\n' "$tmp_root"
fi

pop_bin="${POP_LIVE_POP_BIN:-$tmp_root/pop}"
if [[ -z "${POP_LIVE_POP_BIN:-}" ]]; then
  printf 'Building pop smoke binary: %s\n' "$pop_bin"
  (cd "$repo_root" && go build -o "$pop_bin" ./)
fi

timeout="${POP_LIVE_TIMEOUT:-10m}"
max_tries="${POP_LIVE_MAX_TRIES:-1}"

agent_executable() {
  case "$1" in
    claude) printf 'claude' ;;
    codex) printf 'codex' ;;
    cursor) printf 'cursor-agent' ;;
    opencode) printf 'opencode' ;;
    pi) printf 'pi' ;;
    *)
      printf 'unknown agent preset: %s\n' "$1" >&2
      return 1
      ;;
  esac
}

write_issue_set() {
  local runtime="$1"
  local agent="$2"
  local issue_dir="$runtime/thoughts/issues/live-agent-smoke"

  mkdir -p "$issue_dir"
  cat >"$issue_dir/index.json" <<'JSON'
{
  "issues": [
    {
      "id": "01-live-agent-smoke",
      "file": "01-live-agent-smoke.md",
      "title": "Live agent smoke",
      "type": "AFK",
      "status": "open"
    }
  ]
}
JSON

  cat >"$issue_dir/01-live-agent-smoke.md" <<EOF
# Live agent smoke

## What to build

Create or update \`live-agent-smoke.txt\` in the runtime checkout so it contains exactly this single line:

\`\`\`text
agent-smoke: $agent
\`\`\`

Do not make a git commit.

## Acceptance criteria

- [ ] \`live-agent-smoke.txt\` exists
- [ ] \`live-agent-smoke.txt\` contains exactly \`agent-smoke: $agent\`
EOF
}

init_runtime_repo() {
  local runtime="$1"
  mkdir -p "$runtime"
  git -C "$runtime" init >/dev/null
  git -C "$runtime" config user.email "pop-live-smoke@example.invalid"
  git -C "$runtime" config user.name "pop live smoke"
  printf 'thoughts/\n.xdg/\n' >"$runtime/.gitignore"
  printf '# pop live workload smoke\n' >"$runtime/README.md"
  git -C "$runtime" add -A
  git -C "$runtime" commit -m "init" >/dev/null
}

verify_result() {
  local runtime="$1"
  local agent="$2"
  local expected="agent-smoke: $agent"
  local smoke_file="$runtime/live-agent-smoke.txt"
  local manifest="$runtime/thoughts/issues/live-agent-smoke/index.json"

  if [[ ! -f "$smoke_file" ]]; then
    printf 'missing implementation file: %s\n' "$smoke_file" >&2
    return 1
  fi
  if [[ "$(cat "$smoke_file")" != "$expected" ]]; then
    printf 'unexpected implementation file contents for %s:\n' "$agent" >&2
    cat "$smoke_file" >&2
    return 1
  fi
  if ! grep -q '"status": "done"' "$manifest"; then
    printf 'issue was not marked done for %s:\n' "$agent" >&2
    cat "$manifest" >&2
    return 1
  fi
  git -C "$runtime" log -1 --format='%h %s'
}

failures=()
skips=()
passes=()

for agent in "${agents[@]}"; do
  exe="$(agent_executable "$agent")"
  if ! command -v "$exe" >/dev/null 2>&1; then
    printf 'Skipping %s: executable not found: %s\n' "$agent" "$exe" >&2
    skips+=("$agent")
    continue
  fi

  runtime="$tmp_root/runtime-$agent"
  xdg="$tmp_root/xdg-$agent"
  mkdir -p "$xdg"
  init_runtime_repo "$runtime"
  write_issue_set "$runtime" "$agent"

  printf '\n==> Running live workload smoke for %s in %s\n' "$agent" "$runtime"
  if (
    cd "$runtime"
    XDG_DATA_HOME="$xdg" "$pop_bin" workload \
      --path "$runtime" \
      --workload-definition-path "$runtime" \
      run-issues thoughts/issues/live-agent-smoke \
      --workload-runtime-path "$runtime" \
      --agent "$agent" \
      --agent-output auto \
      --max-tries "$max_tries" \
      --timeout "$timeout" \
      --yes
  ); then
    printf 'Verification commit for %s: ' "$agent"
    if verify_result "$runtime" "$agent"; then
      passes+=("$agent")
    else
      failures+=("$agent")
    fi
  else
    failures+=("$agent")
  fi
done

printf '\n==> Live workload smoke summary\n'
if [[ ${#passes[@]} -gt 0 ]]; then
  printf 'Passed: %s\n' "${passes[*]}"
fi
if [[ ${#skips[@]} -gt 0 ]]; then
  printf 'Skipped: %s\n' "${skips[*]}"
fi
if [[ ${#failures[@]} -gt 0 ]]; then
  printf 'Failed: %s\n' "${failures[*]}" >&2
  exit 1
fi
