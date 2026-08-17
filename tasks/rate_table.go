package tasks

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"time"
)

//go:embed rate_table.json
var rateTableVendoredFS embed.FS

const (
	rateTableVendoredName = "rate_table.json"
	rateTableCacheName    = "rate-table.json"
	rateTableURL          = "https://openrouter.ai/api/v1/models"
	rateTableMaxAge       = 24 * time.Hour
	rateTableFetchTimeout = 3 * time.Second
	rateTableBodyLimit    = 2 << 20
)

// RateTableFetcher fetches the raw OpenRouter models payload. Tests inject a
// fake so the Spend lens never reaches the network (ADR-0218).
type RateTableFetcher interface {
	Fetch(ctx context.Context) ([]byte, error)
}

// RealRateTableFetcher loads OpenRouter's public models endpoint.
type RealRateTableFetcher struct {
	URL     string
	Timeout time.Duration
	Client  *http.Client
}

func (f RealRateTableFetcher) Fetch(ctx context.Context) ([]byte, error) {
	url := f.URL
	if url == "" {
		url = rateTableURL
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = rateTableFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rate table fetch status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, rateTableBodyLimit))
}

func (d *Deps) rateTableFetcher() RateTableFetcher {
	if d != nil && d.RateTableFetcher != nil {
		return d.RateTableFetcher
	}
	return RealRateTableFetcher{}
}

// ModelRates is the four per-token rates a model may publish. CacheWrite may be
// absent — those providers bill cache-write tokens at the prompt rate (ADR-0218).
type ModelRates struct {
	Prompt     float64
	Completion float64
	CacheRead  float64
	CacheWrite float64
	HasCacheWrite bool
}

// RateTable is the priced-model snapshot the Spend lens reads.
type RateTable struct {
	FetchedAt time.Time
	Models    map[string]ModelRates
}

// rateTableFile is the on-disk / embedded JSON shape.
type rateTableFile struct {
	FetchedAt string                     `json:"fetched_at"`
	Models    map[string]rateTableRatesJSON `json:"models"`
}

type rateTableRatesJSON struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	CacheRead  string `json:"cache_read,omitempty"`
	CacheWrite string `json:"cache_write,omitempty"`
}

// openRouterModelsPayload is the live endpoint shape, trimmed on ingest.
type openRouterModelsPayload struct {
	Data []struct {
		ID       string            `json:"id"`
		Pricing  map[string]string `json:"pricing"`
	} `json:"data"`
}

func parseRateTableJSON(raw []byte) (*RateTable, error) {
	var file rateTableFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.FetchedAt == "" || len(file.Models) == 0 {
		return nil, fmt.Errorf("rate table missing fetched_at or models")
	}
	fetchedAt, err := time.Parse(time.RFC3339, file.FetchedAt)
	if err != nil {
		return nil, fmt.Errorf("rate table fetched_at: %w", err)
	}
	models := make(map[string]ModelRates, len(file.Models))
	for id, rawRates := range file.Models {
		rates, err := parseModelRates(rawRates)
		if err != nil {
			return nil, fmt.Errorf("model %s: %w", id, err)
		}
		models[id] = rates
	}
	return &RateTable{FetchedAt: fetchedAt.UTC(), Models: models}, nil
}

func parseModelRates(raw rateTableRatesJSON) (ModelRates, error) {
	prompt, err := parseRateString(raw.Prompt)
	if err != nil {
		return ModelRates{}, fmt.Errorf("prompt: %w", err)
	}
	completion, err := parseRateString(raw.Completion)
	if err != nil {
		return ModelRates{}, fmt.Errorf("completion: %w", err)
	}
	rates := ModelRates{Prompt: prompt, Completion: completion}
	if raw.CacheRead != "" {
		v, err := parseRateString(raw.CacheRead)
		if err != nil {
			return ModelRates{}, fmt.Errorf("cache_read: %w", err)
		}
		rates.CacheRead = v
	}
	if raw.CacheWrite != "" {
		v, err := parseRateString(raw.CacheWrite)
		if err != nil {
			return ModelRates{}, fmt.Errorf("cache_write: %w", err)
		}
		rates.CacheWrite = v
		rates.HasCacheWrite = true
	}
	return rates, nil
}

func parseRateString(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty rate")
	}
	return strconv.ParseFloat(s, 64)
}

func trimOpenRouterModels(raw []byte, fetchedAt time.Time) (*RateTable, []byte, error) {
	var payload openRouterModelsPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, err
	}
	if len(payload.Data) == 0 {
		return nil, nil, fmt.Errorf("openrouter models payload empty")
	}
	models := make(map[string]ModelRates, len(payload.Data))
	fileModels := make(map[string]rateTableRatesJSON, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID == "" || m.Pricing == nil {
			continue
		}
		rawRates := rateTableRatesJSON{
			Prompt:     m.Pricing["prompt"],
			Completion: m.Pricing["completion"],
			CacheRead:  m.Pricing["input_cache_read"],
			CacheWrite: m.Pricing["input_cache_write"],
		}
		if rawRates.Prompt == "" || rawRates.Completion == "" {
			continue
		}
		rates, err := parseModelRates(rawRates)
		if err != nil {
			continue
		}
		models[m.ID] = rates
		fileModels[m.ID] = rawRates
	}
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("openrouter models yielded no usable rates")
	}
	table := &RateTable{FetchedAt: fetchedAt.UTC(), Models: models}
	encoded, err := json.Marshal(rateTableFile{
		FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		Models:    fileModels,
	})
	if err != nil {
		return nil, nil, err
	}
	return table, encoded, nil
}

func loadVendoredRateTable() (*RateTable, error) {
	raw, err := rateTableVendoredFS.ReadFile(rateTableVendoredName)
	if err != nil {
		return nil, err
	}
	return parseRateTableJSON(raw)
}

func rateTableCachePath(d *Deps) string {
	return filepath.Join(popDataDirWith(d), rateTableCacheName)
}

func loadCachedRateTable(d *Deps) (*RateTable, bool) {
	raw, err := d.FS.ReadFile(rateTableCachePath(d))
	if err != nil {
		return nil, false
	}
	table, err := parseRateTableJSON(raw)
	if err != nil {
		return nil, false
	}
	return table, true
}

func writeCachedRateTable(d *Deps, raw []byte) {
	path := rateTableCachePath(d)
	_ = d.FS.MkdirAll(popDataDirWith(d), 0o755)
	_ = d.FS.WriteFile(path, raw, 0o644)
}

// loadRateTableForSpend returns the Rate table in effect for a Spend lens call.
// A cache younger than a day is used as-is. An older (or missing) table triggers
// a refresh behind the injected fetcher; any failure falls back to the vendored
// snapshot and never fails the command (ADR-0218).
func loadRateTableForSpend(d *Deps) *RateTable {
	vendored, err := loadVendoredRateTable()
	if err != nil {
		// A missing embed would be a build bug; keep the lens alive with an empty table.
		return &RateTable{Models: map[string]ModelRates{}}
	}
	now := d.Now().UTC()
	if cached, ok := loadCachedRateTable(d); ok {
		if now.Sub(cached.FetchedAt) < rateTableMaxAge {
			return cached
		}
		if refreshed := refreshRateTable(d, now); refreshed != nil {
			return refreshed
		}
		return vendored
	}
	if now.Sub(vendored.FetchedAt) < rateTableMaxAge {
		return vendored
	}
	if refreshed := refreshRateTable(d, now); refreshed != nil {
		return refreshed
	}
	return vendored
}

func refreshRateTable(d *Deps, now time.Time) *RateTable {
	ctx := context.Background()
	raw, err := d.rateTableFetcher().Fetch(ctx)
	if err != nil {
		return nil
	}
	table, encoded, err := trimOpenRouterModels(raw, now)
	if err != nil {
		return nil
	}
	writeCachedRateTable(d, encoded)
	return table
}

func (t *RateTable) lookup(modelKey string) (ModelRates, bool) {
	if t == nil || modelKey == "" {
		return ModelRates{}, false
	}
	rates, ok := t.Models[modelKey]
	return rates, ok
}

// notionalCostFromRates prices TokenUsage at the given rates. Cache-write tokens
// without a published cache-write rate use the prompt rate.
func notionalCostFromRates(u TokenUsage, rates ModelRates) PartialCost {
	if !u.HasUsage() {
		return PartialCost{}
	}
	var dollars float64
	if u.HasInput {
		dollars += float64(u.Input) * rates.Prompt
	}
	if u.HasOutput {
		dollars += float64(u.Output) * rates.Completion
	}
	if u.HasCacheRead {
		dollars += float64(u.CacheRead) * rates.CacheRead
	}
	if u.HasCacheWrite {
		writeRate := rates.Prompt
		if rates.HasCacheWrite {
			writeRate = rates.CacheWrite
		}
		dollars += float64(u.CacheWrite) * writeRate
	}
	return PartialCost{Dollars: dollars, HasCost: true}
}

// claudeRateKey maps Claude's reported Actual model onto an OpenRouter id.
// Claude streams emit bare Anthropic names (e.g. claude-opus-5); the table is
// keyed with the anthropic/ vendor prefix. Other adapters wait for their
// normalisation rules (ADR-0218 slice 04).
func claudeRateKey(actualModel string) string {
	if actualModel == "" {
		return ""
	}
	for i := 0; i < len(actualModel); i++ {
		if actualModel[i] == '/' {
			return actualModel
		}
	}
	return "anthropic/" + actualModel
}

// priceRunSpend resolves the dollar annotation for one Captured run: pi's
// measured PartialCost outranks a table estimate; this slice estimates Claude
// runs only; everything else is rate-blind (ADR-0218).
func priceRunSpend(run capturedRun, spend RunSpend, table *RateTable) PartialCost {
	if spend.Cost.HasCost {
		return spend.Cost
	}
	if run.meta.Agent != "claude" {
		return PartialCost{}
	}
	key := claudeRateKey(extractActualModel(run.meta.Agent, run.events))
	rates, ok := table.lookup(key)
	if !ok {
		return PartialCost{}
	}
	return notionalCostFromRates(spend.Tokens, rates)
}
