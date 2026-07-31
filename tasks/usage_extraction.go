package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// extractTokenUsage applies the agent's declared Usage capability to a
// Captured run's stored events (ADR-0160, ADR-0165). Blind or unknown
// adapters return a token-blind TokenUsage: a zero-value with no Has* flags,
// distinguishable from a reported zero (Has* true, counts zero).
func extractTokenUsage(agent string, events []streamEventRecord) TokenUsage {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return TokenUsage{}
	}
	cap := adapter.UsageCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return TokenUsage{}
	}
	return cap.Extract(events)
}

// extractPartialCost applies the agent's declared Cost capability. Blind or
// unknown adapters return HasCost false rather than zero dollars.
func extractPartialCost(agent string, events []streamEventRecord) PartialCost {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return PartialCost{}
	}
	cap := adapter.CostCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return PartialCost{}
	}
	return cap.Extract(events)
}

// extractTurnCount applies the agent's declared Turn capability. Blind or
// unknown adapters return HasTurn false rather than zero turns.
func extractTurnCount(agent string, events []streamEventRecord) TurnCount {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return TurnCount{}
	}
	cap := adapter.TurnCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return TurnCount{}
	}
	return cap.Extract(events)
}

// extractPeakInput applies the agent's declared peak-input capability. Blind or
// unknown adapters return HasPeak false rather than zero tokens.
func extractPeakInput(agent string, events []streamEventRecord) PeakInput {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return PeakInput{}
	}
	cap := adapter.PeakInputCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return PeakInput{}
	}
	return cap.Extract(events)
}

// collectSpendRuns loads every Captured run under streams/runs/ for a task
// set. The legacy streams/<task-stem>/attempt-NNN.jsonl.gz layout is out of
// scope for spend (ADR-0160) and is never read here.
func collectSpendRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	var runs []capturedRun
	runsDir := capturedRunsDir(taskSetDir)
	entries, err := d.FS.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		run, err := loadCapturedRun(d, runsDir, e.Name())
		if err != nil {
			return nil, fmt.Errorf("load captured run %s: %w", e.Name(), err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// listSpendRuns returns every Captured run under streams/runs/ for a task
// set, sorted chronologically. Legacy attempt-NNN files are not included.
func listSpendRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	runs, err := collectSpendRuns(d, taskSetDir)
	if err != nil {
		return nil, err
	}
	sortRunsChronologically(runs)
	return runs, nil
}

// runSpend derives Run spend for one Captured run via its adapter's extraction
// rules and applies the over-count guard. Legacy runs are rejected — spend
// never reads them.
func runSpend(run capturedRun) (RunSpend, error) {
	if isLegacyRun(run) {
		return RunSpend{}, fmt.Errorf("spend does not read legacy attempt streams (%s)", filepath.Base(run.legacyPath))
	}
	spend := RunSpend{
		Tokens:    extractTokenUsage(run.meta.Agent, run.events),
		Cost:      extractPartialCost(run.meta.Agent, run.events),
		Turns:     extractTurnCount(run.meta.Agent, run.events),
		PeakInput: extractPeakInput(run.meta.Agent, run.events),
	}
	if err := checkUsageOverCountGuard(run.meta.Agent, run.events, spend.Tokens); err != nil {
		return RunSpend{}, err
	}
	return spend, nil
}
