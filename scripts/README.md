# Scripts

## Live Workload Agent Smoke

`live-workload-agent-smoke.sh` runs `pop tasks implement` against real agent CLIs in disposable git repositories:

```bash
scripts/live-workload-agent-smoke.sh codex
scripts/live-workload-agent-smoke.sh codex claude opencode
POP_LIVE_KEEP=1 POP_LIVE_TIMEOUT=15m scripts/live-workload-agent-smoke.sh pi
```

This is intentionally not part of `go test ./...`: it uses local agent authentication, can consume quota, and depends on installed external CLIs.
