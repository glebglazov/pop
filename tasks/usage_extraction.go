package tasks

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// spendStreamMarkers returns the adapter's spend-line prefilter, or nil when
// the agent is spend-stream-blind (open no events file).
func spendStreamMarkers(agent string) []string {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return nil
	}
	return adapter.SpendStreamMarkers()
}

// loadCapturedRunMeta reads one Captured run's .meta.json without touching its
// event stream.
func loadCapturedRunMeta(d *Deps, dir, metaName string) (capturedRunMeta, error) {
	metaPath := filepath.Join(dir, metaName)
	metaData, err := d.FS.ReadFile(metaPath)
	if err != nil {
		return capturedRunMeta{}, err
	}
	var meta capturedRunMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return capturedRunMeta{}, fmt.Errorf("parse meta: %w", err)
	}
	return meta, nil
}

// listSpendRunMetas returns every Captured run meta under streams/runs/ for a
// task set, sorted chronologically. Event streams are never opened.
func listSpendRunMetas(d *Deps, taskSetDir string) ([]capturedRunMeta, error) {
	runsDir := capturedRunsDir(taskSetDir)
	entries, err := d.FS.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var metas []capturedRunMeta
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		meta, err := loadCapturedRunMeta(d, runsDir, e.Name())
		if err != nil {
			return nil, fmt.Errorf("load captured run meta %s: %w", e.Name(), err)
		}
		metas = append(metas, meta)
	}
	sort.SliceStable(metas, func(i, j int) bool {
		if !metas[i].StartTime.Equal(metas[j].StartTime) {
			return metas[i].StartTime.Before(metas[j].StartTime)
		}
		return phaseOrder(metas[i].Phase) < phaseOrder(metas[j].Phase)
	})
	return metas, nil
}

// collectSpendRuns loads every Captured run under streams/runs/ for a task
// set as meta-only records (no event stream). The legacy
// streams/<task-stem>/attempt-NNN.jsonl.gz layout is out of scope for spend
// (ADR-0160) and is never read here.
func collectSpendRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	metas, err := listSpendRunMetas(d, taskSetDir)
	if err != nil {
		return nil, err
	}
	runs := make([]capturedRun, len(metas))
	for i, meta := range metas {
		runs[i] = capturedRun{meta: meta}
	}
	return runs, nil
}

// listSpendRuns returns every Captured run under streams/runs/ for a task
// set, sorted chronologically, without opening event streams. Call
// loadRunSpend to derive spend for each run.
func listSpendRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	return collectSpendRuns(d, taskSetDir)
}

// persistedRunSpend is the on-disk cache of one terminal run's extracted spend
// (beside the run as <runID>.spend.json). Pricing is not cached — rates drift.
type persistedRunSpend struct {
	Tokens      TokenUsage  `json:"tokens"`
	Cost        PartialCost `json:"cost"`
	Turns       TurnCount   `json:"turns"`
	PeakInput   PeakInput   `json:"peak_input"`
	ActualModel string      `json:"actual_model,omitempty"`
}

func spendCachePath(runsDir, runID string) string {
	return filepath.Join(runsDir, runID+".spend.json")
}

func readPersistedRunSpend(d *Deps, path string) (persistedRunSpend, bool) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return persistedRunSpend{}, false
	}
	var cached persistedRunSpend
	if err := json.Unmarshal(data, &cached); err != nil {
		return persistedRunSpend{}, false
	}
	return cached, true
}

func writePersistedRunSpend(d *Deps, path string, spend RunSpend, actualModel string) {
	payload, err := json.Marshal(persistedRunSpend{
		Tokens:      spend.Tokens,
		Cost:        spend.Cost,
		Turns:       spend.Turns,
		PeakInput:   spend.PeakInput,
		ActualModel: actualModel,
	})
	if err != nil {
		return
	}
	_ = d.FS.WriteFile(path, payload, 0o644)
}

// loadRunSpend derives Run spend for one Captured run for the Spend lens:
// reuse a terminal-run cache when present, otherwise stream-scan the events
// file once with the adapter's marker prefilter. Blind adapters skip the
// stream entirely. A terminal run's result is persisted beside the run.
func loadRunSpend(d *Deps, runsDir string, run capturedRun) (RunSpend, string, error) {
	if isLegacyRun(run) {
		return RunSpend{}, "", fmt.Errorf("spend does not read legacy attempt streams (%s)", filepath.Base(run.legacyPath))
	}
	agent := run.meta.Agent
	markers := spendStreamMarkers(agent)
	if len(markers) == 0 {
		return RunSpend{}, "", nil
	}

	cachePath := spendCachePath(runsDir, run.meta.RunID)
	if run.meta.Outcome != "" {
		if cached, ok := readPersistedRunSpend(d, cachePath); ok {
			return RunSpend{
				Tokens:    cached.Tokens,
				Cost:      cached.Cost,
				Turns:     cached.Turns,
				PeakInput: cached.PeakInput,
			}, cached.ActualModel, nil
		}
	}

	events, err := scanSpendEvents(d, runsDir, run.meta.RunID, markers)
	if err != nil {
		return RunSpend{}, "", err
	}
	spend := RunSpend{
		Tokens:    extractTokenUsage(agent, events),
		Cost:      extractPartialCost(agent, events),
		Turns:     extractTurnCount(agent, events),
		PeakInput: extractPeakInput(agent, events),
	}
	if err := checkUsageOverCountGuard(agent, events, spend.Tokens); err != nil {
		return RunSpend{}, "", err
	}
	actual := extractActualModel(agent, events)
	if run.meta.Outcome != "" {
		writePersistedRunSpend(d, cachePath, spend, actual)
	}
	return spend, actual, nil
}

// scanSpendEvents streams one Captured run's .events.jsonl.gz, keeping only
// lines that match a spend marker and discarding the rest without JSON-parsing
// them. The decompressed stream is never held whole in memory.
func scanSpendEvents(d *Deps, runsDir, runID string, markers []string) ([]streamEventRecord, error) {
	name := runID + ".events.jsonl.gz"
	f, err := d.FS.DirFS(runsDir).Open(name)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompress events: %w", err)
	}
	defer zr.Close()

	markerBytes := make([][]byte, len(markers))
	for i, m := range markers {
		markerBytes[i] = []byte(m)
	}

	var events []streamEventRecord
	br := bufio.NewReaderSize(zr, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 && lineMatchesSpendMarker(trimmed, markerBytes) {
				var ev streamEventRecord
				if err := json.Unmarshal(trimmed, &ev); err != nil {
					return nil, fmt.Errorf("parse event: %w", err)
				}
				events = append(events, ev)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decompress events: %w", err)
		}
	}
	return events, nil
}

func lineMatchesSpendMarker(line []byte, markers [][]byte) bool {
	for _, m := range markers {
		if bytes.Contains(line, m) {
			return true
		}
	}
	return false
}

// runSpend derives Run spend for one Captured run via its adapter's extraction
// rules and applies the over-count guard. Legacy runs are rejected — spend
// never reads them. Callers that already hold events (tests, fixtures) use
// this; the Spend lens prefers loadRunSpend.
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
