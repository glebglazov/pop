package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// usageExtractionRules maps agent preset → Usage extraction rule (ADR-0160).
// A rule states where a stream's authoritative usage lives and whether those
// figures accumulate or replace — it is deliberately not a field-name
// translation table. An adapter absent from this map yields a Token-blind
// run: a zero-value TokenUsage with no Has* flags, distinguishable from a
// reported zero (Has* true, counts zero).
var usageExtractionRules = map[string]func([]streamEventRecord) TokenUsage{
	"claude": claudeTokenUsage,
	"cursor": cursorTokenUsage,
	"pi":     piTokenUsage,
}

// extractTokenUsage applies the agent's Usage extraction rule to a Captured
// run's stored events. Unknown adapters return a token-blind TokenUsage.
func extractTokenUsage(agent string, events []streamEventRecord) TokenUsage {
	if rule := usageExtractionRules[agent]; rule != nil {
		return rule(events)
	}
	return TokenUsage{}
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

// runSpendTokens derives TokenUsage for one Captured run via its adapter's
// Usage extraction rule. Legacy runs are rejected — spend never reads them.
func runSpendTokens(run capturedRun) (TokenUsage, error) {
	if isLegacyRun(run) {
		return TokenUsage{}, fmt.Errorf("spend does not read legacy attempt streams (%s)", filepath.Base(run.legacyPath))
	}
	return extractTokenUsage(run.meta.Agent, run.events), nil
}
