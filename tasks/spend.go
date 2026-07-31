package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// spendRollupSetLimit is the display bound for bare `pop tasks spend`: the
// ten most recent Task sets, not a claim about substrate depth (ADR-0160).
const spendRollupSetLimit = 10

// SpendOptions configures the Spend lens.
type SpendOptions struct {
	ResolveInput
	// Target is a bare Task set identifier for per-set breakdown. Empty selects
	// the cross-set rollup.
	Target string
}

// SpendRollupRow is aggregated Run spend for one Task set.
type SpendRollupRow struct {
	TaskSetID      string
	Tokens         TokenUsage
	Cost           PartialCost
	RunCount       int
	TokenBlindRuns int
}

// SpendRollupResult is the cross-set spend rollup.
type SpendRollupResult struct {
	Sets []SpendRollupRow
}

// spendRollupJSONRow is the machine-readable rollup row emitted by --json.
type spendRollupJSONRow struct {
	TaskSetID        string   `json:"task_set_id"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	PartialCostUSD   *float64 `json:"partial_cost_usd,omitempty"`
	RunCount         int      `json:"run_count"`
	TokenBlindRuns   int      `json:"token_blind_runs"`
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
	RunCount       int
	TokenBlindRuns int
}

// SpendSetBreakdownResult is the per-task spend breakdown for one Task set.
type SpendSetBreakdownResult struct {
	TaskSetID                  string
	Rows                       []SpendBreakdownRow
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
	RunCount         int      `json:"run_count"`
	TokenBlindRuns   int      `json:"token_blind_runs"`
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
	Rows                         []spendBreakdownJSONRow `json:"rows"`
}

// SpendRollup aggregates Run spend across the most recent Task sets. It is a
// read-only lens: nothing is captured and nothing is mutated (ADR-0160).
func SpendRollup(opts SpendOptions) (*SpendRollupResult, error) {
	return SpendRollupWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SpendRollupWith aggregates Run spend using injected dependencies.
func SpendRollupWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SpendOptions) (*SpendRollupResult, error) {
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

	setIDs := recentTaskSetIDsForSpend(state, resolved.DefinitionPath, refresh.Manifests, spendRollupSetLimit)
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

	sort.Slice(result.Sets, func(i, j int) bool {
		return tokenUsageTotal(result.Sets[i].Tokens) > tokenUsageTotal(result.Sets[j].Tokens)
	})
	return result, nil
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
	for _, run := range runs {
		spend, _, err := runSpend(run)
		if err != nil {
			return nil, err
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
		addTokenUsage(&row.Tokens, spend.Tokens)
		addPartialCost(&row.Cost, spend.Cost)

		if key == spendVerifyRowKey {
			result.VerificationRunCount++
			if !spend.Tokens.HasUsage() {
				result.VerificationTokenBlindRuns++
			}
			addTokenUsage(&result.VerificationTokens, spend.Tokens)
			addPartialCost(&result.VerificationCost, spend.Cost)
		} else {
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
	return result, nil
}

const spendVerifyRowKey = "__verify__"

func spendBreakdownRowKey(run capturedRun, m *Manifest) (key, taskID, title string) {
	if run.meta.Phase == "verify" {
		return spendVerifyRowKey, "verify", "Verify"
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

// recentTaskSetIDsForSpend returns up to limit non-archived Task set identifiers
// ordered newest-first by reverse identifier sort (the same recency heuristic
// as transfer export completion).
func recentTaskSetIDsForSpend(state *GlobalState, defPath string, manifests map[string]*Manifest, limit int) []string {
	archived := archivedTaskSetIDs(state, defPath)
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		if archived[id] {
			continue
		}
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	if len(ids) > limit {
		ids = ids[:limit]
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
	for _, run := range runs {
		spend, _, err := runSpend(run)
		if err != nil {
			return SpendRollupRow{}, err
		}
		row.RunCount++
		if !spend.Tokens.HasUsage() {
			row.TokenBlindRuns++
		}
		addTokenUsage(&row.Tokens, spend.Tokens)
		addPartialCost(&row.Cost, spend.Cost)
	}
	return row, nil
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
	if showCost {
		fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5s %6s %12s\n",
			"task set", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	} else {
		fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5s %6s\n",
			"task set", "in", "out", "cache-r", "cache-w", "runs", "blind")
	}
	for _, row := range result.Sets {
		if showCost {
			fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5d %6d %12s\n",
				row.TaskSetID,
				formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
				formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
				formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
				formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
				row.RunCount,
				row.TokenBlindRuns,
				formatPartialCost(row.Cost),
			)
		} else {
			fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5d %6d\n",
				row.TaskSetID,
				formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
				formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
				formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
				formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
				row.RunCount,
				row.TokenBlindRuns,
			)
		}
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
	fmt.Fprintf(w, "%s\n\n", verifyLine)

	showCost := spendBreakdownHasPartialCost(result)
	if showCost {
		fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5s %6s %12s\n",
			"task", "in", "out", "cache-r", "cache-w", "runs", "blind", "cost (partial)")
	} else {
		fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5s %6s\n",
			"task", "in", "out", "cache-r", "cache-w", "runs", "blind")
	}
	for _, row := range result.Rows {
		if showCost {
			fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5d %6d %12s\n",
				row.TaskID,
				formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
				formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
				formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
				formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
				row.RunCount,
				row.TokenBlindRuns,
				formatPartialCost(row.Cost),
			)
		} else {
			fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5d %6d\n",
				row.TaskID,
				formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
				formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
				formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
				formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
				row.RunCount,
				row.TokenBlindRuns,
			)
		}
	}
}

func spendBreakdownHasPartialCost(result *SpendSetBreakdownResult) bool {
	if result == nil {
		return false
	}
	if result.ImplementCost.HasCost || result.VerificationCost.HasCost {
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
		Rows:                         make([]spendBreakdownJSONRow, len(result.Rows)),
	}
	for i, row := range result.Rows {
		payload.Rows[i] = spendBreakdownJSONRow{
			TaskID:           row.TaskID,
			Title:            row.Title,
			InputTokens:      row.Tokens.Input,
			OutputTokens:     row.Tokens.Output,
			CacheReadTokens:  row.Tokens.CacheRead,
			CacheWriteTokens: row.Tokens.CacheWrite,
			PartialCostUSD:   partialCostUSDPtr(row.Cost),
			RunCount:         row.RunCount,
			TokenBlindRuns:   row.TokenBlindRuns,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// RenderSpendRollupJSON writes the cross-set spend rollup as JSON.
func RenderSpendRollupJSON(w io.Writer, result *SpendRollupResult) error {
	payload := spendRollupJSON{Sets: make([]spendRollupJSONRow, len(result.Sets))}
	for i, row := range result.Sets {
		payload.Sets[i] = spendRollupJSONRow{
			TaskSetID:        row.TaskSetID,
			InputTokens:      row.Tokens.Input,
			OutputTokens:     row.Tokens.Output,
			CacheReadTokens:  row.Tokens.CacheRead,
			CacheWriteTokens: row.Tokens.CacheWrite,
			PartialCostUSD:   partialCostUSDPtr(row.Cost),
			RunCount:         row.RunCount,
			TokenBlindRuns:   row.TokenBlindRuns,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
