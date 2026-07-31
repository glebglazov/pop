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

// SpendOptions configures the Spend lens rollup.
type SpendOptions struct {
	ResolveInput
}

// SpendRollupRow is aggregated Run spend for one Task set.
type SpendRollupRow struct {
	TaskSetID      string
	Tokens         TokenUsage
	RunCount       int
	TokenBlindRuns int
}

// SpendRollupResult is the cross-set spend rollup.
type SpendRollupResult struct {
	Sets []SpendRollupRow
}

// spendRollupJSONRow is the machine-readable rollup row emitted by --json.
type spendRollupJSONRow struct {
	TaskSetID       string `json:"task_set_id"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	RunCount        int    `json:"run_count"`
	TokenBlindRuns  int    `json:"token_blind_runs"`
}

// spendRollupJSON is the machine-readable rollup payload.
type spendRollupJSON struct {
	Sets []spendRollupJSONRow `json:"sets"`
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
		tokens, _, err := runSpendTokens(run)
		if err != nil {
			return SpendRollupRow{}, err
		}
		row.RunCount++
		if !tokens.HasUsage() {
			row.TokenBlindRuns++
		}
		addTokenUsage(&row.Tokens, tokens)
	}
	return row, nil
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
	fmt.Fprintf(w, "%-28s %8s %8s %9s %9s %5s %6s\n",
		"task set", "in", "out", "cache-r", "cache-w", "runs", "blind")
	for _, row := range result.Sets {
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

func formatSpendCount(reported bool, n int64) string {
	if !reported {
		return "—"
	}
	return fmt.Sprintf("%d", n)
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
			RunCount:         row.RunCount,
			TokenBlindRuns:   row.TokenBlindRuns,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
