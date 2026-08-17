package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// spendRollupSetLimit is the display bound for bare `pop tasks spend`: the
// ten most recent Task sets, not a claim about substrate depth (ADR-0160).
const spendRollupSetLimit = 10

// Spend sort vocabulary for the cross-set rollup (ADR-0218).
const (
	SpendSortRecency = "recency"
	SpendSortTokens  = "tokens"
)

// SpendOptions configures the Spend lens.
type SpendOptions struct {
	ResolveInput
	// Target is a bare Task set identifier for per-set breakdown. Empty selects
	// the cross-set rollup.
	Target string
	// Sort orders the cross-set rollup. Empty means recency. Unrecognised
	// values are refused before anything is rendered.
	Sort string
}

// SpendRollupRow is aggregated Run spend for one Task set.
type SpendRollupRow struct {
	TaskSetID      string
	Tokens         TokenUsage
	Cost           PartialCost
	Turns          TurnCount
	PeakInput      PeakInput
	RunCount       int
	TokenBlindRuns int
	TurnBlindRuns  int
	PeakBlindRuns  int
	Agents         string // populated when the set mixes agents
	// LastRunAt is the latest Captured run start time in the set. Zero when no
	// run carries a readable start time (ADR-0218).
	LastRunAt time.Time
}

// SpendRollupResult is the cross-set spend rollup.
type SpendRollupResult struct {
	Sets       []SpendRollupRow
	ShowAgents bool // true when any displayed set mixes agents
}

// spendRollupJSONRow is the machine-readable rollup row emitted by --json.
type spendRollupJSONRow struct {
	TaskSetID        string     `json:"task_set_id"`
	InputTokens      int64      `json:"input_tokens"`
	OutputTokens     int64      `json:"output_tokens"`
	CacheReadTokens  int64      `json:"cache_read_tokens"`
	CacheWriteTokens int64      `json:"cache_write_tokens"`
	PartialCostUSD   *float64   `json:"partial_cost_usd,omitempty"`
	Turns            *int       `json:"turns"`
	TurnBlindRuns    int        `json:"turn_blind_runs"`
	PeakInputTokens  *int64     `json:"peak_input_tokens"`
	PeakBlindRuns    int        `json:"peak_blind_runs"`
	RunCount         int        `json:"run_count"`
	TokenBlindRuns   int        `json:"token_blind_runs"`
	LastRunAt        *time.Time `json:"last_run_at"`
	Agent            string     `json:"agent,omitempty"`
}

// spendRollupJSON is the machine-readable rollup payload.
type spendRollupJSON struct {
	Sets []spendRollupJSONRow `json:"sets"`
}

// SpendBreakdownRow is aggregated Run spend for one task or verification row.
type SpendBreakdownRow struct {
	TaskID         string
	Title          string
	Tokens         TokenUsage
	Cost           PartialCost
	Turns          TurnCount
	PeakInput      PeakInput
	RunCount       int
	TokenBlindRuns int
	TurnBlindRuns  int
	PeakBlindRuns  int
	Agent          string // populated when the set mixes agents
}

// SpendSetBreakdownResult is the per-task spend breakdown for one Task set.
type SpendSetBreakdownResult struct {
	TaskSetID                  string
	Rows                       []SpendBreakdownRow
	ShowAgents                 bool // true when the set mixes agents
	CompletedTasks             int
	TokensPerCompletedTask     *int64
	ImplementTokens            TokenUsage
	ImplementCost              PartialCost
	ImplementRunCount          int
	ImplementTokenBlindRuns    int
	VerificationTokens         TokenUsage
	VerificationCost           PartialCost
	VerificationRunCount       int
	VerificationTokenBlindRuns int
	// The Review bucket is the third phase a set spends agent quota in
	// (ADR-0214). It is reported only when the set has been reviewed, so a set
	// that never was reads exactly as it did before review existed.
	ReviewTokens         TokenUsage
	ReviewCost           PartialCost
	ReviewRunCount       int
	ReviewTokenBlindRuns int
}

// spendBreakdownJSONRow is the machine-readable breakdown row emitted by --json.
type spendBreakdownJSONRow struct {
	TaskID           string   `json:"task_id"`
	Title            string   `json:"title"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	PartialCostUSD   *float64 `json:"partial_cost_usd,omitempty"`
	Turns            *int     `json:"turns"`
	TurnBlindRuns    int      `json:"turn_blind_runs"`
	PeakInputTokens  *int64   `json:"peak_input_tokens"`
	PeakBlindRuns    int      `json:"peak_blind_runs"`
	RunCount         int      `json:"run_count"`
	TokenBlindRuns   int      `json:"token_blind_runs"`
	Agent            string   `json:"agent,omitempty"`
}

// spendSetBreakdownJSON is the machine-readable per-set breakdown payload.
type spendSetBreakdownJSON struct {
	TaskSetID                    string                  `json:"task_set_id"`
	CompletedTasks               int                     `json:"completed_tasks"`
	TokensPerCompletedTask       *int64                  `json:"tokens_per_completed_task,omitempty"`
	ImplementInputTokens         int64                   `json:"implement_input_tokens"`
	ImplementOutputTokens        int64                   `json:"implement_output_tokens"`
	ImplementCacheReadTokens     int64                   `json:"implement_cache_read_tokens"`
	ImplementCacheWriteTokens    int64                   `json:"implement_cache_write_tokens"`
	ImplementPartialCostUSD      *float64                `json:"implement_partial_cost_usd,omitempty"`
	ImplementRunCount            int                     `json:"implement_run_count"`
	ImplementTokenBlindRuns      int                     `json:"implement_token_blind_runs"`
	VerificationInputTokens      int64                   `json:"verification_input_tokens"`
	VerificationOutputTokens     int64                   `json:"verification_output_tokens"`
	VerificationCacheReadTokens  int64                   `json:"verification_cache_read_tokens"`
	VerificationCacheWriteTokens int64                   `json:"verification_cache_write_tokens"`
	VerificationPartialCostUSD   *float64                `json:"verification_partial_cost_usd,omitempty"`
	VerificationRunCount         int                     `json:"verification_run_count"`
	VerificationTokenBlindRuns   int                     `json:"verification_token_blind_runs"`
	ReviewInputTokens            int64                   `json:"review_input_tokens"`
	ReviewOutputTokens           int64                   `json:"review_output_tokens"`
	ReviewCacheReadTokens        int64                   `json:"review_cache_read_tokens"`
	ReviewCacheWriteTokens       int64                   `json:"review_cache_write_tokens"`
	ReviewPartialCostUSD         *float64                `json:"review_partial_cost_usd,omitempty"`
	ReviewRunCount               int                     `json:"review_run_count"`
	ReviewTokenBlindRuns         int                     `json:"review_token_blind_runs"`
	Rows                         []spendBreakdownJSONRow `json:"rows"`
}

// SpendRollup aggregates Run spend across the most recent Task sets. It is a
// read-only lens: nothing is captured and nothing is mutated (ADR-0160).
func SpendRollup(opts SpendOptions) (*SpendRollupResult, error) {
	return SpendRollupWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SpendRollupWith aggregates Run spend using injected dependencies.
func SpendRollupWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SpendOptions) (*SpendRollupResult, error) {
	sortMode, err := normalizeSpendSort(opts.Sort)
	if err != nil {
		return nil, err
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	state, err := LoadGlobalStateWith(d, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	setIDs := activeTaskSetIDsForSpend(state, resolved.DefinitionPath, refresh.Manifests)
	result := &SpendRollupResult{Sets: make([]SpendRollupRow, 0, len(setIDs))}
	for _, setID := range setIDs {
		m := refresh.Manifests[setID]
		if m == nil {
			continue
		}
		row, err := taskSetSpendRollup(d, setID, m.Dir)
		if err != nil {
			return nil, exitErr(ExitOperational, "spend for %s: %v", setID, err)
		}
		result.Sets = append(result.Sets, row)
	}

	// The display bound is always the most recent N by Captured-run start time;
	// --sort then reorders that window (ADR-0218).
	sortSpendRollupRows(result.Sets, SpendSortRecency)
	if len(result.Sets) > spendRollupSetLimit {
		result.Sets = result.Sets[:spendRollupSetLimit]
	}
	if sortMode != SpendSortRecency {
		sortSpendRollupRows(result.Sets, sortMode)
	}
	for _, row := range result.Sets {
		if spendAgentsMix(row.Agents) {
			result.ShowAgents = true
			break
		}
	}
	return result, nil
}

// normalizeSpendSort maps the caller's --sort value onto the closed vocabulary.
// Empty means recency. An unrecognised value is refused with the accepted list
// and nothing is rendered.
func normalizeSpendSort(raw string) (string, error) {
	switch raw {
	case "", SpendSortRecency:
		return SpendSortRecency, nil
	case SpendSortTokens:
		return SpendSortTokens, nil
	default:
		return "", exitErr(ExitSetup, "unknown spend sort %q (want one of: %s)", raw, strings.Join([]string{SpendSortRecency, SpendSortTokens}, ", "))
	}
}

func sortSpendRollupRows(rows []SpendRollupRow, mode string) {
	sort.SliceStable(rows, func(i, j int) bool {
		return spendRollupRowLess(rows[i], rows[j], mode)
	})
}

// spendRollupRowLess reports whether a should sort before b. Recency is every
// sort's tie-break: newer Captured-run start first, and a set with no readable
// start time always sorts after one that has one.
func spendRollupRowLess(a, b SpendRollupRow, mode string) bool {
	if mode == SpendSortTokens {
		ta, tb := tokenUsageTotal(a.Tokens), tokenUsageTotal(b.Tokens)
		if ta != tb {
			return ta > tb
		}
	}
	return spendRollupRecencyLess(a, b)
}

func spendRollupRecencyLess(a, b SpendRollupRow) bool {
	aZero, bZero := a.LastRunAt.IsZero(), b.LastRunAt.IsZero()
	if aZero != bZero {
		return !aZero
	}
	if !aZero && !a.LastRunAt.Equal(b.LastRunAt) {
		return a.LastRunAt.After(b.LastRunAt)
	}
	return a.TaskSetID < b.TaskSetID
}

// SpendSetBreakdown aggregates Run spend for one Task set, broken down per task
// with verification runs on their own rows. It is a read-only lens (ADR-0160).
func SpendSetBreakdown(opts SpendOptions) (*SpendSetBreakdownResult, error) {
	return SpendSetBreakdownWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SpendSetBreakdownWith aggregates per-task Run spend using injected dependencies.
func SpendSetBreakdownWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SpendOptions) (*SpendSetBreakdownResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetID, taskID, err := ResolveTaskTarget(refresh, opts.Target)
	if err != nil {
		return nil, err
	}
	if taskSetID == "" {
		return nil, exitErr(ExitSetup, "spend breakdown requires a task set identifier")
	}
	if taskID != "" {
		return nil, exitErr(ExitSetup, "spend breakdown requires a bare task set identifier, not <task-set>/<file>.md")
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}

	result, err := buildSpendSetBreakdown(d, taskSetID, m)
	if err != nil {
		return nil, exitErr(ExitOperational, "spend for %s: %v", taskSetID, err)
	}
	return result, nil
}

func buildSpendSetBreakdown(d *Deps, taskSetID string, m *Manifest) (*SpendSetBreakdownResult, error) {
	runs, err := listSpendRuns(d, m.Dir)
	if err != nil {
		return nil, err
	}

	result := &SpendSetBreakdownResult{TaskSetID: taskSetID}
	seen := map[string]int{}
	setAgents := map[string]bool{}
	for _, run := range runs {
		spend, err := runSpend(run)
		if err != nil {
			return nil, err
		}
		if run.meta.Agent != "" {
			setAgents[run.meta.Agent] = true
		}

		key, taskID, title := spendBreakdownRowKey(run, m)
		idx, ok := seen[key]
		if !ok {
			result.Rows = append(result.Rows, SpendBreakdownRow{TaskID: taskID, Title: title})
			idx = len(result.Rows) - 1
			seen[key] = idx
		}
		row := &result.Rows[idx]
		row.RunCount++
		if !spend.Tokens.HasUsage() {
			row.TokenBlindRuns++
		}
		if !spend.Turns.HasTurn {
			row.TurnBlindRuns++
		}
		if !spend.PeakInput.HasPeak {
			row.PeakBlindRuns++
		}
		addTokenUsage(&row.Tokens, spend.Tokens)
		addPartialCost(&row.Cost, spend.Cost)
		addTurnCount(&row.Turns, spend.Turns)
		addPeakInput(&row.PeakInput, spend.PeakInput)
		recordSpendRowAgent(row, run.meta.Agent)

		switch key {
		case spendVerifyRowKey:
			result.VerificationRunCount++
			if !spend.Tokens.HasUsage() {
				result.VerificationTokenBlindRuns++
			}
			addTokenUsage(&result.VerificationTokens, spend.Tokens)
			addPartialCost(&result.VerificationCost, spend.Cost)
		case spendReviewRowKey:
			result.ReviewRunCount++
			if !spend.Tokens.HasUsage() {
				result.ReviewTokenBlindRuns++
			}
			addTokenUsage(&result.ReviewTokens, spend.Tokens)
			addPartialCost(&result.ReviewCost, spend.Cost)
		default:
			result.ImplementRunCount++
			if !spend.Tokens.HasUsage() {
				result.ImplementTokenBlindRuns++
			}
			addTokenUsage(&result.ImplementTokens, spend.Tokens)
			addPartialCost(&result.ImplementCost, spend.Cost)
		}
	}

	result.CompletedTasks = countCompletedTasks(m)
	if result.CompletedTasks > 0 {
		perTask := tokenUsageTotal(result.ImplementTokens) / int64(result.CompletedTasks)
		result.TokensPerCompletedTask = &perTask
	}
	result.ShowAgents = len(setAgents) > 1
	return result, nil
}

const (
	spendVerifyRowKey = "__verify__"
	spendReviewRowKey = "__review__"
)

func spendBreakdownRowKey(run capturedRun, m *Manifest) (key, taskID, title string) {
	if run.meta.Phase == "verify" {
		return spendVerifyRowKey, "verify", "Verify"
	}
	if run.meta.Phase == "review" {
		return spendReviewRowKey, "review", "Review"
	}
	key = run.meta.TaskFile
	if task := taskByFile(m, key); task != nil {
		return key, task.ID, task.Title
	}
	return key, run.meta.TaskID, key
}

func countCompletedTasks(m *Manifest) int {
	if m == nil {
		return 0
	}
	n := 0
	for _, task := range m.Tasks {
		if task.Status == TaskDone {
			n++
		}
	}
	return n
}

// activeTaskSetIDsForSpend returns every non-archived Task set identifier. The
// rollup picks its display window by Captured-run start time after aggregation
// (ADR-0218); identifier order is not a stand-in for recency.
func activeTaskSetIDsForSpend(state *GlobalState, defPath string, manifests map[string]*Manifest) []string {
	archived := archivedTaskSetIDs(state, defPath)
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		if archived[id] {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func archivedTaskSetIDs(state *GlobalState, defPath string) map[string]bool {
	out := map[string]bool{}
	if state == nil {
		return out
	}
	entry := state.Tasks[defPath]
	if entry == nil {
		return out
	}
	for _, reg := range entry.TaskSets {
		if reg.Archived {
			out[reg.ID] = true
		}
	}
	return out
}

func taskSetSpendRollup(d *Deps, taskSetID, taskSetDir string) (SpendRollupRow, error) {
	runs, err := listSpendRuns(d, taskSetDir)
	if err != nil {
		return SpendRollupRow{}, err
	}
	row := SpendRollupRow{TaskSetID: taskSetID}
	setAgents := map[string]bool{}
	for _, run := range runs {
		spend, err := runSpend(run)
		if err != nil {
			return SpendRollupRow{}, err
		}
		if run.meta.Agent != "" {
			setAgents[run.meta.Agent] = true
		}
		if !run.meta.StartTime.IsZero() && (row.LastRunAt.IsZero() || run.meta.StartTime.After(row.LastRunAt)) {
			row.LastRunAt = run.meta.StartTime
		}
		row.RunCount++
		if !spend.Tokens.HasUsage() {
			row.TokenBlindRuns++
		}
		if !spend.Turns.HasTurn {
			row.TurnBlindRuns++
		}
		if !spend.PeakInput.HasPeak {
			row.PeakBlindRuns++
		}
		addTokenUsage(&row.Tokens, spend.Tokens)
		addPartialCost(&row.Cost, spend.Cost)
		addTurnCount(&row.Turns, spend.Turns)
		addPeakInput(&row.PeakInput, spend.PeakInput)
	}
	row.Agents = formatSpendAgents(setAgents)
	return row, nil
}

func spendLastRunAtJSONPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func addPartialCost(acc *PartialCost, c PartialCost) {
	if c.HasCost {
		acc.Dollars += c.Dollars
		acc.HasCost = true
	}
}

func addTokenUsage(acc *TokenUsage, u TokenUsage) {
	if u.HasInput {
		acc.Input += u.Input
		acc.HasInput = true
	}
	if u.HasOutput {
		acc.Output += u.Output
		acc.HasOutput = true
	}
	if u.HasCacheRead {
		acc.CacheRead += u.CacheRead
		acc.HasCacheRead = true
	}
	if u.HasCacheWrite {
		acc.CacheWrite += u.CacheWrite
		acc.HasCacheWrite = true
	}
}

func addTurnCount(acc *TurnCount, tc TurnCount) {
	if !tc.HasTurn {
		return
	}
	acc.Count += tc.Count
	acc.HasTurn = true
}

func addPeakInput(acc *PeakInput, peak PeakInput) {
	if !peak.HasPeak {
		return
	}
	if !acc.HasPeak || peak.Tokens > acc.Tokens {
		acc.Tokens = peak.Tokens
	}
	acc.HasPeak = true
}

func recordSpendRowAgent(row *SpendBreakdownRow, agent string) {
	if agent == "" {
		return
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(row.Agent, ",") {
		if part != "" {
			seen[part] = true
		}
	}
	seen[agent] = true
	row.Agent = formatSpendAgents(seen)
}

func formatSpendAgents(agents map[string]bool) string {
	if len(agents) == 0 {
		return ""
	}
	parts := make([]string, 0, len(agents))
	for agent := range agents {
		parts = append(parts, agent)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func spendAgentsMix(agents string) bool {
	if agents == "" {
		return false
	}
	return strings.Contains(agents, ",")
}

func tokenUsageTotal(u TokenUsage) int64 {
	var total int64
	if u.HasInput {
		total += u.Input
	}
	if u.HasOutput {
		total += u.Output
	}
	if u.HasCacheRead {
		total += u.CacheRead
	}
	if u.HasCacheWrite {
		total += u.CacheWrite
	}
	return total
}

// RenderSpendRollup writes the cross-set spend rollup for humans.
func RenderSpendRollup(w io.Writer, result *SpendRollupResult) {
	showCost := spendRollupHasPartialCost(result)
	showAgents := result != nil && result.ShowAgents
	writeSpendRollupHeader(w, showCost, showAgents)
	for _, row := range result.Sets {
		writeSpendRollupRow(w, row, showCost, showAgents)
	}
}

func writeSpendRollupHeader(w io.Writer, showCost, showAgents bool) {
	switch {
	case showCost && showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s %12s\n",
			"task set", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	case showCost:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5s %6s %12s\n",
			"task set", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	case showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s\n",
			"task set", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind")
	default:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5s %6s\n",
			"task set", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind")
	}
}

func writeSpendRollupRow(w io.Writer, row SpendRollupRow, showCost, showAgents bool) {
	agent := ""
	if showAgents {
		agent = row.Agents
	}
	switch {
	case showCost && showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d %12s\n",
			row.TaskSetID,
			agent,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
			formatPartialCost(row.Cost),
		)
	case showCost:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5d %6d %12s\n",
			row.TaskSetID,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
			formatPartialCost(row.Cost),
		)
	case showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d\n",
			row.TaskSetID,
			agent,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
		)
	default:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5d %6d\n",
			row.TaskSetID,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
		)
	}
}

func spendRollupHasPartialCost(result *SpendRollupResult) bool {
	if result == nil {
		return false
	}
	for _, row := range result.Sets {
		if row.Cost.HasCost {
			return true
		}
	}
	return false
}

func formatSpendCount(reported bool, n int64) string {
	if !reported {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

func formatSpendTurns(tc TurnCount) string {
	if !tc.HasTurn {
		return "—"
	}
	return fmt.Sprintf("%d", tc.Count)
}

func formatSpendPeakInput(peak PeakInput) string {
	if !peak.HasPeak {
		return "—"
	}
	return fmt.Sprintf("%d", peak.Tokens)
}

func spendTurnsJSONPtr(tc TurnCount) *int {
	if !tc.HasTurn {
		return nil
	}
	v := tc.Count
	return &v
}

func spendPeakInputJSONPtr(peak PeakInput) *int64 {
	if !peak.HasPeak {
		return nil
	}
	v := peak.Tokens
	return &v
}

// RenderSpendSetBreakdown writes the per-set spend breakdown for humans.
func RenderSpendSetBreakdown(w io.Writer, result *SpendSetBreakdownResult) {
	fmt.Fprintf(w, "tokens per completed task: %s", formatSpendPerCompletedTask(result))
	implementLine := fmt.Sprintf("  (%s implement", formatSpendTotal(result.ImplementTokens))
	if result.ImplementCost.HasCost {
		implementLine += fmt.Sprintf(", %s partial cost", formatPartialCost(result.ImplementCost))
	}
	fmt.Fprintf(w, "%s, %d done, %d runs, %d blind)\n",
		implementLine,
		result.CompletedTasks,
		result.ImplementRunCount,
		result.ImplementTokenBlindRuns,
	)
	verifyLine := fmt.Sprintf("verification spend: %s", formatSpendTotal(result.VerificationTokens))
	if result.VerificationCost.HasCost {
		verifyLine += fmt.Sprintf("  (%s partial cost", formatPartialCost(result.VerificationCost))
		verifyLine += fmt.Sprintf(", %d runs, %d blind)", result.VerificationRunCount, result.VerificationTokenBlindRuns)
	} else {
		verifyLine += fmt.Sprintf("  (%d runs, %d blind)", result.VerificationRunCount, result.VerificationTokenBlindRuns)
	}
	fmt.Fprintf(w, "%s\n", verifyLine)
	// A set nobody reviewed says nothing about review: the line appears the moment
	// there is spend behind it and never before.
	if result.ReviewRunCount > 0 {
		reviewLine := fmt.Sprintf("review spend: %s", formatSpendTotal(result.ReviewTokens))
		if result.ReviewCost.HasCost {
			reviewLine += fmt.Sprintf("  (%s partial cost", formatPartialCost(result.ReviewCost))
			reviewLine += fmt.Sprintf(", %d runs, %d blind)", result.ReviewRunCount, result.ReviewTokenBlindRuns)
		} else {
			reviewLine += fmt.Sprintf("  (%d runs, %d blind)", result.ReviewRunCount, result.ReviewTokenBlindRuns)
		}
		fmt.Fprintf(w, "%s\n", reviewLine)
	}
	fmt.Fprintln(w)

	showCost := spendBreakdownHasPartialCost(result)
	showAgents := result != nil && result.ShowAgents
	writeSpendBreakdownHeader(w, showCost, showAgents)
	for _, row := range result.Rows {
		writeSpendBreakdownRow(w, row, showCost, showAgents)
	}
}

func writeSpendBreakdownHeader(w io.Writer, showCost, showAgents bool) {
	switch {
	case showCost && showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s %12s\n",
			"task", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	case showCost:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5s %6s %12s\n",
			"task", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	case showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s\n",
			"task", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind")
	default:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5s %6s\n",
			"task", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind")
	}
}

func writeSpendBreakdownRow(w io.Writer, row SpendBreakdownRow, showCost, showAgents bool) {
	agent := ""
	if showAgents {
		agent = row.Agent
	}
	switch {
	case showCost && showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d %12s\n",
			row.TaskID,
			agent,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
			formatPartialCost(row.Cost),
		)
	case showCost:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5d %6d %12s\n",
			row.TaskID,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
			formatPartialCost(row.Cost),
		)
	case showAgents:
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d\n",
			row.TaskID,
			agent,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
		)
	default:
		fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5d %6d\n",
			row.TaskID,
			formatSpendTurns(row.Turns),
			formatSpendPeakInput(row.PeakInput),
			formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
			formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
			formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
			formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
			row.RunCount,
			row.TokenBlindRuns,
		)
	}
}

func spendBreakdownHasPartialCost(result *SpendSetBreakdownResult) bool {
	if result == nil {
		return false
	}
	if result.ImplementCost.HasCost || result.VerificationCost.HasCost || result.ReviewCost.HasCost {
		return true
	}
	for _, row := range result.Rows {
		if row.Cost.HasCost {
			return true
		}
	}
	return false
}

func formatSpendPerCompletedTask(result *SpendSetBreakdownResult) string {
	if result.TokensPerCompletedTask == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *result.TokensPerCompletedTask)
}

func formatSpendTotal(u TokenUsage) string {
	if !u.HasUsage() {
		return "—"
	}
	return fmt.Sprintf("%d", tokenUsageTotal(u))
}

func formatPartialCost(c PartialCost) string {
	if !c.HasCost {
		return "—"
	}
	return fmt.Sprintf("$%.4f", c.Dollars)
}

func partialCostUSDPtr(c PartialCost) *float64 {
	if !c.HasCost {
		return nil
	}
	v := c.Dollars
	return &v
}

// RenderSpendSetBreakdownJSON writes the per-set spend breakdown as JSON.
func RenderSpendSetBreakdownJSON(w io.Writer, result *SpendSetBreakdownResult) error {
	payload := spendSetBreakdownJSON{
		TaskSetID:                    result.TaskSetID,
		CompletedTasks:               result.CompletedTasks,
		TokensPerCompletedTask:       result.TokensPerCompletedTask,
		ImplementInputTokens:         result.ImplementTokens.Input,
		ImplementOutputTokens:        result.ImplementTokens.Output,
		ImplementCacheReadTokens:     result.ImplementTokens.CacheRead,
		ImplementCacheWriteTokens:    result.ImplementTokens.CacheWrite,
		ImplementPartialCostUSD:      partialCostUSDPtr(result.ImplementCost),
		ImplementRunCount:            result.ImplementRunCount,
		ImplementTokenBlindRuns:      result.ImplementTokenBlindRuns,
		VerificationInputTokens:      result.VerificationTokens.Input,
		VerificationOutputTokens:     result.VerificationTokens.Output,
		VerificationCacheReadTokens:  result.VerificationTokens.CacheRead,
		VerificationCacheWriteTokens: result.VerificationTokens.CacheWrite,
		VerificationPartialCostUSD:   partialCostUSDPtr(result.VerificationCost),
		VerificationRunCount:         result.VerificationRunCount,
		VerificationTokenBlindRuns:   result.VerificationTokenBlindRuns,
		ReviewInputTokens:            result.ReviewTokens.Input,
		ReviewOutputTokens:           result.ReviewTokens.Output,
		ReviewCacheReadTokens:        result.ReviewTokens.CacheRead,
		ReviewCacheWriteTokens:       result.ReviewTokens.CacheWrite,
		ReviewPartialCostUSD:         partialCostUSDPtr(result.ReviewCost),
		ReviewRunCount:               result.ReviewRunCount,
		ReviewTokenBlindRuns:         result.ReviewTokenBlindRuns,
		Rows:                         make([]spendBreakdownJSONRow, len(result.Rows)),
	}
	for i, row := range result.Rows {
		jr := spendBreakdownJSONRow{
			TaskID:           row.TaskID,
			Title:            row.Title,
			InputTokens:      row.Tokens.Input,
			OutputTokens:     row.Tokens.Output,
			CacheReadTokens:  row.Tokens.CacheRead,
			CacheWriteTokens: row.Tokens.CacheWrite,
			PartialCostUSD:   partialCostUSDPtr(row.Cost),
			Turns:            spendTurnsJSONPtr(row.Turns),
			TurnBlindRuns:    row.TurnBlindRuns,
			PeakInputTokens:  spendPeakInputJSONPtr(row.PeakInput),
			PeakBlindRuns:    row.PeakBlindRuns,
			RunCount:         row.RunCount,
			TokenBlindRuns:   row.TokenBlindRuns,
		}
		if result.ShowAgents {
			jr.Agent = row.Agent
		}
		payload.Rows[i] = jr
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// RenderSpendRollupJSON writes the cross-set spend rollup as JSON.
func RenderSpendRollupJSON(w io.Writer, result *SpendRollupResult) error {
	payload := spendRollupJSON{Sets: make([]spendRollupJSONRow, len(result.Sets))}
	for i, row := range result.Sets {
		jr := spendRollupJSONRow{
			TaskSetID:        row.TaskSetID,
			InputTokens:      row.Tokens.Input,
			OutputTokens:     row.Tokens.Output,
			CacheReadTokens:  row.Tokens.CacheRead,
			CacheWriteTokens: row.Tokens.CacheWrite,
			PartialCostUSD:   partialCostUSDPtr(row.Cost),
			Turns:            spendTurnsJSONPtr(row.Turns),
			TurnBlindRuns:    row.TurnBlindRuns,
			PeakInputTokens:  spendPeakInputJSONPtr(row.PeakInput),
			PeakBlindRuns:    row.PeakBlindRuns,
			RunCount:         row.RunCount,
			TokenBlindRuns:   row.TokenBlindRuns,
			LastRunAt:        spendLastRunAtJSONPtr(row.LastRunAt),
		}
		if result.ShowAgents {
			jr.Agent = row.Agents
		}
		payload.Sets[i] = jr
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
