package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestVendoredRateTableCarriesEveryModelTrimmed(t *testing.T) {
	table, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Models) < 400 {
		t.Fatalf("vendored models = %d, want the full OpenRouter set", len(table.Models))
	}
	rates, ok := table.lookup("anthropic/claude-opus-5")
	if !ok {
		t.Fatal("missing anthropic/claude-opus-5")
	}
	if rates.Prompt == 0 || rates.Completion == 0 || rates.CacheRead == 0 || !rates.HasCacheWrite {
		t.Fatalf("opus rates incomplete: %+v", rates)
	}
	noWrite, ok := table.lookup("qwen/qwen3.8-27b")
	if !ok {
		t.Fatal("missing qwen/qwen3.8-27b")
	}
	if noWrite.HasCacheWrite {
		t.Fatalf("qwen should omit cache-write: %+v", noWrite)
	}
}

func TestNotionalCostFromRatesUsesPromptForMissingCacheWrite(t *testing.T) {
	rates := ModelRates{Prompt: 0.001, Completion: 0.002, CacheRead: 0.0001}
	u := TokenUsage{CacheWrite: 10, HasCacheWrite: true}
	got := notionalCostFromRates(u, rates)
	if !got.HasCost || got.Dollars != 0.01 {
		t.Fatalf("got %+v, want $0.01 at prompt rate", got)
	}
}

func TestFormatSpendCellRateBlindUsesEmDash(t *testing.T) {
	u := TokenUsage{Input: 100, Output: 50, HasInput: true, HasOutput: true}
	got := formatSpendCell(u, PartialCost{}, "", "")
	if got != "150 (—)" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "$0") {
		t.Fatalf("rate-blind must not render a zero dollar figure: %q", got)
	}
	priced := formatSpendCell(u, PartialCost{Dollars: 1.234, HasCost: true}, RateSourceTable, RateKeyFromActual)
	if priced != "150 (~$1.23)" {
		t.Fatalf("priced = %q", priced)
	}
	requested := formatSpendCell(u, PartialCost{Dollars: 1.234, HasCost: true}, RateSourceTable, RateKeyFromRequested)
	if requested != "150 (~?$1.23)" {
		t.Fatalf("requested = %q", requested)
	}
	declared := formatSpendCell(u, PartialCost{Dollars: 1.234, HasCost: true}, RateSourceOverride, RateKeyFromActual)
	if declared != "150 (*~$1.23)" {
		t.Fatalf("declared = %q", declared)
	}
}

type scriptedRateTableFetcher struct {
	calls int
	body  []byte
	err   error
}

func (f *scriptedRateTableFetcher) Fetch(context.Context) ([]byte, error) {
	f.calls++
	return f.body, f.err
}

func TestLoadRateTableFreshCacheSkipsFetch(t *testing.T) {
	d := newTestDeps(t)
	now := time.Date(2026, 8, 17, 18, 0, 0, 0, time.UTC)
	d.Clock = deps.FixedClock{Instant: now}
	fetcher := &scriptedRateTableFetcher{err: errors.New("should not fetch")}
	d.RateTableFetcher = fetcher

	raw, err := json.Marshal(rateTableFile{
		FetchedAt: now.Add(-time.Hour).Format(time.RFC3339),
		Models: map[string]rateTableRatesJSON{
			"anthropic/claude-opus-5": {Prompt: "0.001", Completion: "0.002"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeCachedRateTable(d, raw)

	table := loadRateTableForSpend(d)
	if fetcher.calls != 0 {
		t.Fatalf("fetch calls = %d, want 0 for fresh cache", fetcher.calls)
	}
	if _, ok := table.lookup("anthropic/claude-opus-5"); !ok {
		t.Fatal("expected cached model")
	}
}

func TestLoadRateTableStaleCacheRefreshes(t *testing.T) {
	d := newTestDeps(t)
	now := time.Date(2026, 8, 17, 18, 0, 0, 0, time.UTC)
	d.Clock = deps.FixedClock{Instant: now}

	stale, err := json.Marshal(rateTableFile{
		FetchedAt: now.Add(-25 * time.Hour).Format(time.RFC3339),
		Models: map[string]rateTableRatesJSON{
			"old/model": {Prompt: "0.001", Completion: "0.002"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeCachedRateTable(d, stale)

	payload := []byte(`{"data":[{"id":"anthropic/claude-opus-5","pricing":{"prompt":"0.000005","completion":"0.000025","input_cache_read":"0.0000005","input_cache_write":"0.00000625"}}]}`)
	fetcher := &scriptedRateTableFetcher{body: payload}
	d.RateTableFetcher = fetcher

	table := loadRateTableForSpend(d)
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
	if _, ok := table.lookup("anthropic/claude-opus-5"); !ok {
		t.Fatal("expected refreshed model")
	}
	if _, ok := table.lookup("old/model"); ok {
		t.Fatal("stale model should be replaced")
	}
	cachedRaw, err := d.FS.ReadFile(rateTableCachePath(d))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cachedRaw), "anthropic/claude-opus-5") {
		t.Fatalf("cache not written under data dir: %s", cachedRaw)
	}
	popData := popDataDirWith(d)
	if !strings.HasPrefix(rateTableCachePath(d), popData) {
		t.Fatalf("cache path %q not under data dir %q", rateTableCachePath(d), popData)
	}
}

func TestLoadRateTableRefreshFailureFallsBackToVendored(t *testing.T) {
	d := newTestDeps(t)
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC) // vendored snapshot is older than a day
	d.Clock = deps.FixedClock{Instant: now}
	fetcher := &scriptedRateTableFetcher{err: errors.New("timeout")}
	d.RateTableFetcher = fetcher

	table := loadRateTableForSpend(d)
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
	if _, ok := table.lookup("anthropic/claude-opus-5"); !ok {
		t.Fatal("expected vendored fallback")
	}
	vendored, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}
	if !table.FetchedAt.Equal(vendored.FetchedAt) {
		t.Fatalf("fetched_at = %v, want vendored %v", table.FetchedAt, vendored.FetchedAt)
	}
}

func TestLoadRateTableGarbageFallsBackToVendored(t *testing.T) {
	d := newTestDeps(t)
	d.Clock = deps.FixedClock{Instant: time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)}
	fetcher := &scriptedRateTableFetcher{body: []byte(`{"not":"models"}`)}
	d.RateTableFetcher = fetcher

	table := loadRateTableForSpend(d)
	if _, ok := table.lookup("anthropic/claude-opus-5"); !ok {
		t.Fatal("expected vendored fallback after garbage")
	}
}

func TestRealRateTableFetcherHonoursTimeout(t *testing.T) {
	f := RealRateTableFetcher{Timeout: rateTableFetchTimeout}
	if f.Timeout != 3*time.Second {
		t.Fatalf("timeout = %v, want 3s", f.Timeout)
	}
	// Zero Timeout on the real type falls back to the 3s constant in Fetch.
	var zero RealRateTableFetcher
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := zero.Fetch(ctx)
	if err == nil {
		t.Fatal("expected cancelled context to fail without hanging")
	}
}

func TestSpendRollupPricesClaudeRunNotional(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-priced", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(1000, 500, 100, 50, "claude-opus-5"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sets) != 1 {
		t.Fatalf("sets = %#v", result.Sets)
	}
	row := result.Sets[0]
	if !row.Notional.HasCost {
		t.Fatalf("expected notional cost, got %+v", row.Notional)
	}
	// 1000*0.000005 + 500*0.000025 + 100*0.0000005 + 50*0.00000625
	want := 1000*0.000005 + 500*0.000025 + 100*0.0000005 + 50*0.00000625
	if diff := row.Notional.Dollars - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("notional = %v, want %v", row.Notional.Dollars, want)
	}

	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	cell := formatSpendCell(row.Tokens, row.Notional, row.RateSource, row.ModelKeySource)
	if !strings.Contains(out, cell) {
		t.Fatalf("render missing spend cell %q:\n%s", cell, out)
	}
	if !strings.Contains(out, "rate table 2026-08-17") {
		t.Fatalf("footer missing rate table date:\n%s", out)
	}
}

func TestSpendRollupUnknownModelIsRateBlind(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-blind", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(10, 5, 0, 0, "claude-does-not-exist"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if row.Notional.HasCost {
		t.Fatalf("expected rate-blind, got %+v", row.Notional)
	}
	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	if !strings.Contains(buf.String(), "15 (—)") {
		t.Fatalf("expected (—) cell:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "~$0.00") {
		t.Fatalf("must not render zero dollars:\n%s", buf.String())
	}
}

func TestSpendSetBreakdownPricesClaudeAndPrefersPiMeasured(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-mix", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "pi", base, piCostEvents(0.42))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), claudePricedEvents(1000, 0, 0, 0, "claude-opus-5"))

	result, err := SpendSetBreakdownWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "2026-06-10-mix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ImplementNotional.HasCost {
		t.Fatal("expected implement notional")
	}
	// pi measured 0.42 outranks any estimate for that run; claude adds 1000*0.000005
	want := 0.42 + 1000*0.000005
	if diff := result.ImplementNotional.Dollars - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("implement notional = %v, want %v", result.ImplementNotional.Dollars, want)
	}
	var buf bytes.Buffer
	RenderSpendSetBreakdown(&buf, result)
	out := buf.String()
	if !strings.Contains(out, formatSpendCell(result.ImplementTokens, result.ImplementNotional, "", "")) {
		t.Fatalf("breakdown missing spend cell:\n%s", out)
	}
	if !strings.Contains(out, "rate table ") {
		t.Fatalf("breakdown missing rate table footer:\n%s", out)
	}
}

func claudePricedEvents(input, output, cacheRead, cacheWrite int64, model string) []streamEventRecord {
	usage := fmt.Sprintf(
		`{"type":"result","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}`,
		input, output, cacheRead, cacheWrite,
	)
	return []streamEventRecord{
		{Type: "event", AtMS: 50, Raw: fmt.Sprintf(`{"type":"system","subtype":"init","model":%q}`, model)},
		{Type: "event", AtMS: 100, Raw: usage},
	}
}
