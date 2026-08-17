package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClaudeRateKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"claude-opus-5", "anthropic/claude-opus-5"},
		{"anthropic/claude-opus-5", "anthropic/claude-opus-5"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := claudeRateKey(tt.in); got != tt.want {
			t.Fatalf("claudeRateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCursorRateKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"claude-opus-5-thinking-medium", "anthropic/claude-opus-5"},
		{"claude-opus-5-thinking-low", "anthropic/claude-opus-5"},
		{"claude-sonnet-5-thinking-high", "anthropic/claude-sonnet-5"},
		{"cursor-grok-4.5-high", "x-ai/grok-4.5"},
		{"cursor-grok-4.5-medium", "x-ai/grok-4.5"},
		{"composer-2.5", "composer-2.5"},
		{"composer-2.5-fast", "composer-2.5-fast"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cursorRateKey(tt.in); got != tt.want {
			t.Fatalf("cursorRateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPiRateKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"opencode-go/kimi-k3", "moonshotai/kimi-k3"},
		{"opencode-go/deepseek-v4-flash", "deepseek/deepseek-v4-flash"},
		{"opencode-go/kimi-k2.7-code", "moonshotai/kimi-k2.7-code"},
		{"opencode-go/qwen3.7-max", "opencode-go/qwen3.7-max"},
		{"moonshotai/kimi-k3", "moonshotai/kimi-k3"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := piRateKey(tt.in); got != tt.want {
			t.Fatalf("piRateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCodexRateKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"gpt-5.6-sol", "openai/gpt-5.6-sol"},
		{"gpt-5.6-terra", "openai/gpt-5.6-terra"},
		{"openai/gpt-5.6-sol", "openai/gpt-5.6-sol"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := codexRateKey(tt.in); got != tt.want {
			t.Fatalf("codexRateKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRateKeyRulesAreExactNotSubstring(t *testing.T) {
	// A name that merely contains a frontier id must not be rewritten into that
	// frontier's rate key — substring matching is how a cheap model gets priced
	// as an expensive one (ADR-0218).
	if got := cursorRateKey("not-really-claude-opus-5-thinking-medium-extra"); got == "anthropic/claude-opus-5" {
		t.Fatalf("cursorRateKey approximated via substring: %q", got)
	}
	if got := piRateKey("other/kimi-k3-lookalike"); got == "moonshotai/kimi-k3" {
		t.Fatalf("piRateKey approximated via substring: %q", got)
	}
}

func TestResolveRunRateKeyDistinguishesActualFromRequested(t *testing.T) {
	table, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}

	actualRun := capturedRun{
		meta: capturedRunMeta{Agent: "cursor"},
		events: []streamEventRecord{
			{Type: "event", AtMS: 1, Raw: `{"type":"system","subtype":"init","model":"claude-opus-5-thinking-medium"}`},
		},
	}
	got := resolveRunRateKey(actualRun, table)
	if got.Key != "anthropic/claude-opus-5" || got.Source != RateKeyFromActual {
		t.Fatalf("actual resolve = %+v, want key anthropic/claude-opus-5 source actual", got)
	}

	requestedRun := capturedRun{
		meta: capturedRunMeta{
			Agent:          "codex",
			RequestedAgent: "codex --model gpt-5.6-sol",
		},
	}
	got = resolveRunRateKey(requestedRun, table)
	if got.Key != "openai/gpt-5.6-sol" || got.Source != RateKeyFromRequested {
		t.Fatalf("requested resolve = %+v, want key openai/gpt-5.6-sol source requested", got)
	}
}

func TestResolveRunRateKeyAdapterWithoutRuleIsBlind(t *testing.T) {
	table, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}
	// kimi ships no rate-key rule; even a table-resident model stays blind.
	run := capturedRun{
		meta: capturedRunMeta{
			Agent:          "kimi",
			RequestedAgent: "kimi --model moonshotai/kimi-k3",
		},
	}
	got := resolveRunRateKey(run, table)
	if got.Key != "" || got.Source != "" {
		t.Fatalf("expected rate-blind without a rule, got %+v", got)
	}
}

func TestResolveRunRateKeyComposerStaysBlind(t *testing.T) {
	table, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"composer-2.5", "composer-2.5-fast"} {
		run := capturedRun{
			meta: capturedRunMeta{Agent: "cursor"},
			events: []streamEventRecord{
				{Type: "event", AtMS: 1, Raw: fmt.Sprintf(`{"type":"system","subtype":"init","model":%q}`, model)},
			},
		}
		got := resolveRunRateKey(run, table)
		if got.Key != model {
			t.Fatalf("%s key = %q, want normalized %q", model, got.Key, model)
		}
		priced := priceRunSpend(run, RunSpend{
			Tokens: TokenUsage{Input: 100, HasInput: true},
		}, table, nil)
		if priced.Cost.HasCost || priced.RateSource != "" {
			t.Fatalf("%s should stay rate-blind without a declaration, got %+v", model, priced)
		}
	}
}

func TestSpendRollupPricesCursorAnthropicAndGrok(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-cursor", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base, cursorPricedEvents(1000, 0, 0, 0, "claude-opus-5-thinking-medium"))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base.Add(time.Minute), cursorPricedEvents(1000, 0, 0, 0, "cursor-grok-4.5-high"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if !row.Notional.HasCost {
		t.Fatalf("expected notional, got %+v", row.Notional)
	}
	// anthropic/claude-opus-5 prompt 0.000005 + x-ai/grok-4.5 prompt 0.000002
	want := 1000*0.000005 + 1000*0.000002
	if diff := row.Notional.Dollars - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("notional = %v, want %v", row.Notional.Dollars, want)
	}
}

func TestSpendRollupPricesPiViaNamespaceMap(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-pi", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	// No measured cost — table estimate must apply through the namespace map.
	writeSpendRun(t, setDir, "01-a.md", "01-a", "pi", base, piPricedEvents(1000, 0, 0, 0, "opencode-go/kimi-k3"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if !row.Notional.HasCost {
		t.Fatalf("expected notional, got %+v", row.Notional)
	}
	want := 1000 * 0.000003 // moonshotai/kimi-k3 prompt
	if diff := row.Notional.Dollars - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("notional = %v, want %v", row.Notional.Dollars, want)
	}
}

func TestSpendRollupPricesCodexFromRequestedAgent(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-codex", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRunEx(t, setDir, "01-a.md", "01-a", "codex", base, codexPricedEvents(1000, 0), spendRunOpts{
		requestedAgent: "codex --model gpt-5.6-sol",
	})

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if !row.Notional.HasCost {
		t.Fatalf("expected notional from requested model, got %+v", row.Notional)
	}
	if row.ModelKeySource != RateKeyFromRequested {
		t.Fatalf("model_key_source = %q, want requested", row.ModelKeySource)
	}
	if row.ModelKey != "openai/gpt-5.6-sol" {
		t.Fatalf("model_key = %q, want openai/gpt-5.6-sol", row.ModelKey)
	}
	want := 1000 * 0.0000025 // openai/gpt-5.6-sol prompt
	if diff := row.Notional.Dollars - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("notional = %v, want %v", row.Notional.Dollars, want)
	}

	table, err := loadVendoredRateTable()
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolveRunRateKey(capturedRun{
		meta: capturedRunMeta{
			Agent:          "codex",
			RequestedAgent: "codex --model gpt-5.6-sol",
		},
	}, table)
	if resolved.Source != RateKeyFromRequested || resolved.Key != "openai/gpt-5.6-sol" {
		t.Fatalf("resolved = %+v, want requested openai/gpt-5.6-sol", resolved)
	}

	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	if !strings.Contains(buf.String(), "(~?$") {
		t.Fatalf("expected requested-model spend marker:\n%s", buf.String())
	}
	var jsonBuf bytes.Buffer
	if err := RenderSpendRollupJSON(&jsonBuf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Sets[0]
	if got.ModelKeySource == nil || *got.ModelKeySource != string(RateKeyFromRequested) {
		t.Fatalf("model_key_source = %v, want requested", got.ModelKeySource)
	}
}

func TestSpendRollupComposerCountedButRateBlind(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-composer", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base, cursorPricedEvents(100, 50, 0, 0, "composer-2.5"))
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base.Add(time.Minute), cursorPricedEvents(10, 5, 0, 0, "composer-2.5-fast"))

	result, err := SpendRollupWith(env.deps(), nil, nil, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if row.Notional.HasCost {
		t.Fatalf("composer must stay rate-blind, got %+v", row.Notional)
	}
	if row.RunCount != 2 {
		t.Fatalf("run_count = %d, want 2 (counted)", row.RunCount)
	}
	if got := row.Tokens.Input + row.Tokens.Output; got != 165 {
		t.Fatalf("tokens = %d, want 165", got)
	}
	var buf strings.Builder
	RenderSpendRollup(&buf, result)
	if !strings.Contains(buf.String(), "(—)") {
		t.Fatalf("expected (—) cell:\n%s", buf.String())
	}
}

func cursorPricedEvents(input, output, cacheRead, cacheWrite int64, model string) []streamEventRecord {
	usage := fmt.Sprintf(
		`{"type":"result","usage":{"inputTokens":%d,"outputTokens":%d,"cacheReadTokens":%d,"cacheWriteTokens":%d}}`,
		input, output, cacheRead, cacheWrite,
	)
	return []streamEventRecord{
		{Type: "event", AtMS: 50, Raw: fmt.Sprintf(`{"type":"system","subtype":"init","model":%q}`, model)},
		{Type: "event", AtMS: 100, Raw: usage},
	}
}

func piPricedEvents(input, output, cacheRead, cacheWrite int64, model string) []streamEventRecord {
	return []streamEventRecord{
		{Type: "event", AtMS: 50, Raw: fmt.Sprintf(
			`{"type":"message_end","message":{"role":"assistant","model":%q,"usage":{"input":%d,"output":%d,"cacheRead":%d,"cacheWrite":%d}}}`,
			model, input, output, cacheRead, cacheWrite,
		)},
	}
}

func codexPricedEvents(input, output int64) []streamEventRecord {
	return []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: fmt.Sprintf(
			`{"type":"turn.completed","usage":{"input_tokens":%d,"output_tokens":%d}}`,
			input, output,
		)},
	}
}
