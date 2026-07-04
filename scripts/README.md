# Scripts

## Live Agent Smoke

`live-agent-smoke.sh` runs `pop tasks implement` against real agent CLIs in disposable git repositories:

```bash
scripts/live-agent-smoke.sh codex
scripts/live-agent-smoke.sh codex claude opencode
POP_LIVE_KEEP=1 POP_LIVE_TIMEOUT=15m scripts/live-agent-smoke.sh pi
```

This is intentionally not part of `go test ./...`: it uses local agent authentication, can consume quota, and depends on installed external CLIs.
