package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/fanout"
	"github.com/glebglazov/pop/internal/repokey"
	"github.com/glebglazov/pop/project"
)

// spendRollupSetLimit is the display bound for bare `pop tasks spend`: the
// ten most recent Task sets of the current repository (ADR-0160).
const spendRollupSetLimit = 10

// spendRollupAllSetLimit is the display bound for `pop tasks spend --all`:
// the twenty most recent Task sets across every registration on this machine
// (ADR-0218). Overridable with --limit; never a per-project quota.
const spendRollupAllSetLimit = 20

// Spend sort vocabulary for the cross-set rollup (ADR-0218).
const (
	SpendSortRecency = "recency"
	SpendSortTokens  = "tokens"
	SpendSortCost    = "cost"
)

// Rate-source labels for machine-readable Notional cost provenance (ADR-0218).
const (
	RateSourceTable    = "table"
	RateSourceOverride = "override"
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
	// All widens the rollup across every Task-set registration on this machine
	// (ADR-0218). Without it the lens stays repo-scoped.
	All bool
	// Limit caps how many rows the rollup keeps after the recency window is
	// taken. Zero means the scope default (10 repo-scoped, 20 with --all).
	Limit int
}

// SpendRollupRow is aggregated Run spend for one Task set.
type SpendRollupRow struct {
	TaskSetID      string
	// Project is the repository-root basename for this set, disambiguated by a
	// parent segment when two projects share a basename (ADR-0218). Present in
	// both scopes so a payload shape never depends on --all.
	Project        string
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
	// Notional is the dollar annotation for this row: pi's measured PartialCost
	// where present, else a Rate-table estimate via the adapter's rate-key rule.
	// HasCost false is rate-blind — absent, never zero (ADR-0218).
	Notional PartialCost
	// RateSource is "table" or "override" when a published or declared rate
	// priced any run; empty when the figure is measured-only or the row is
	// rate-blind (ADR-0218).
	RateSource string
	// ModelKey is the Rate-table key the row was priced under. Empty when
	// rate-blind or when priced runs disagree on the key.
	ModelKey string
}

// SpendRollupResult is the cross-set spend rollup.
type SpendRollupResult struct {
	Sets       []SpendRollupRow
	ShowAgents bool // true when any displayed set mixes agents
	// Sort is the ordering applied to Sets after the recency window (ADR-0218).
	// Render uses it to label the rate-blind trailing block under --sort cost.
	Sort string
	// SkippedStorages is how many AllSets def_paths were skipped because their
	// Task storage directory is gone. Reported in the human footer so a total
	// never silently shrinks (ADR-0218).
	SkippedStorages int
	// RateTableDate is the fetched_at of the Rate table that priced this render.
	RateTableDate time.Time
}

// spendRollupJSONRow is the machine-readable rollup row emitted by --json.
type spendRollupJSONRow struct {
	TaskSetID        string     `json:"task_set_id"`
	Project          string     `json:"project"`
	InputTokens      int64      `json:"input_tokens"`
	OutputTokens     int64      `json:"output_tokens"`
	CacheReadTokens  int64      `json:"cache_read_tokens"`
	CacheWriteTokens int64      `json:"cache_write_tokens"`
	PartialCostUSD   *float64   `json:"partial_cost_usd,omitempty"`
	NotionalCostUSD  *float64   `json:"notional_cost_usd,omitempty"`
	RateSource       *string    `json:"rate_source"`
	ModelKey         string     `json:"model_key"`
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
	Notional       PartialCost
	RateSource     string
	ModelKey       string
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
	ImplementNotional    PartialCost
	VerificationNotional PartialCost
	ReviewNotional       PartialCost
	// RateTableDate is the fetched_at of the Rate table that priced this render.
	RateTableDate time.Time
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
	NotionalCostUSD  *float64 `json:"notional_cost_usd,omitempty"`
	RateSource       *string  `json:"rate_source"`
	ModelKey         string   `json:"model_key"`
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
	if opts.Limit < 0 {
		return nil, exitErr(ExitSetup, "spend limit must be >= 0")
	}

	table := loadRateTableForSpend(d)

	var result *SpendRollupResult
	if opts.All {
		result, err = spendRollupAll(d, table)
	} else {
		result, err = spendRollupRepo(d, pd, loadConfig, opts, table)
	}
	if err != nil {
		return nil, err
	}
	result.RateTableDate = table.FetchedAt
	result.Sort = sortMode

	limit := spendRollupSetLimit
	if opts.All {
		limit = spendRollupAllSetLimit
	}
	if opts.Limit > 0 {
		limit = opts.Limit
	}

	// The display bound is always the most recent N by Captured-run start time;
	// --sort then reorders that window (ADR-0218).
	sortSpendRollupRows(result.Sets, SpendSortRecency)
	if len(result.Sets) > limit {
		result.Sets = result.Sets[:limit]
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

// spendRollupRepo is the current-checkout rollup: manifests under one
// definition path, archived sets filtered out (ADR-0160).
func spendRollupRepo(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SpendOptions, table *RateTable) (*SpendRollupResult, error) {
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

	projectLabel := spendProjectLabels(d, []string{resolved.DefinitionPath})[resolved.DefinitionPath]
	setIDs := activeTaskSetIDsForSpend(state, resolved.DefinitionPath, refresh.Manifests)
	result := &SpendRollupResult{Sets: make([]SpendRollupRow, 0, len(setIDs))}
	for _, setID := range setIDs {
		m := refresh.Manifests[setID]
		if m == nil {
			continue
		}
		row, err := taskSetSpendRollup(d, setID, m.Dir, table)
		if err != nil {
			return nil, exitErr(ExitOperational, "spend for %s: %v", setID, err)
		}
		row.Project = projectLabel
		result.Sets = append(result.Sets, row)
	}
	return result, nil
}

// spendRollupAll widens the rollup across every Task-set registration on this
// machine (ADR-0218). Enumeration is store.AllSets — not the config project
// list — grouped by def_path; a def_path whose storage is gone is skipped and
// counted rather than crashing the render.
func spendRollupAll(d *Deps, table *RateTable) (*SpendRollupResult, error) {
	s, ok, err := d.Store(false)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	if !ok {
		return &SpendRollupResult{}, nil
	}
	all, err := s.AllSets()
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	defPaths := make([]string, 0, len(all))
	for def := range all {
		defPaths = append(defPaths, def)
	}
	sort.Strings(defPaths)

	liveDefs := make([]string, 0, len(defPaths))
	skipped := 0
	for _, def := range defPaths {
		_, err := d.FS.Stat(def)
		if os.IsNotExist(err) {
			skipped++
			continue
		}
		if err != nil {
			return nil, exitErr(ExitOperational, "stat task storage %s: %v", def, err)
		}
		liveDefs = append(liveDefs, def)
	}

	result := &SpendRollupResult{SkippedStorages: skipped}
	if len(liveDefs) == 0 {
		return result, nil
	}

	prepared, err := PrepareRefreshes(d, liveDefs)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	refreshes, err := fanout.Map(prepared, func(p *PreparedRefresh) (*RefreshResult, error) {
		return p.Refresh(d)
	})
	if err != nil {
		return nil, exitErr(ExitOperational, "%v", err)
	}

	labels := spendProjectLabels(d, liveDefs)
	for i, def := range liveDefs {
		label := labels[def]
		refresh := refreshes[i]
		for _, reg := range all[def] {
			if reg.Archived {
				continue
			}
			m := refresh.Manifests[reg.SetID]
			if m == nil {
				continue
			}
			row, err := taskSetSpendRollup(d, reg.SetID, m.Dir, table)
			if err != nil {
				return nil, exitErr(ExitOperational, "spend for %s: %v", reg.SetID, err)
			}
			row.Project = label
			result.Sets = append(result.Sets, row)
		}
	}
	return result, nil
}

// spendProjectRef is one definition path's raw project coordinates used to
// build the display label: repository-root basename, disambiguated by a parent
// segment when two collide — never the managed-worktree repoKey (ADR-0218).
type spendProjectRef struct {
	defPath  string
	basename string
	rootPath string
}

func spendProjectLabels(d *Deps, defPaths []string) map[string]string {
	refs := make([]spendProjectRef, 0, len(defPaths))
	for _, def := range defPaths {
		refs = append(refs, readSpendProjectRef(d, def))
	}
	return disambiguateSpendProjectLabels(refs)
}

func readSpendProjectRef(d *Deps, defPath string) spendProjectRef {
	ref := spendProjectRef{defPath: defPath}
	storageDir := filepath.Dir(defPath)
	data, err := d.FS.ReadFile(filepath.Join(storageDir, repoMarkerFile))
	if err == nil {
		var marker RepoMarker
		if json.Unmarshal(data, &marker) == nil && marker.RepositoryPath != "" {
			ref.basename = RepoBasename(marker.RepositoryPath)
			ref.rootPath = spendRepoRoot(marker.RepositoryPath)
			return ref
		}
	}
	ref.basename = repokey.Basename(filepath.Base(storageDir))
	ref.rootPath = storageDir
	return ref
}

func spendRepoRoot(commonDir string) string {
	base := filepath.Base(commonDir)
	switch base {
	case ".git", ".bare":
		return filepath.Dir(commonDir)
	}
	return strings.TrimSuffix(commonDir, ".git")
}

func disambiguateSpendProjectLabels(refs []spendProjectRef) map[string]string {
	out := make(map[string]string, len(refs))
	byBase := map[string][]int{}
	for i, r := range refs {
		byBase[r.basename] = append(byBase[r.basename], i)
	}
	for base, idxs := range byBase {
		if len(idxs) == 1 {
			out[refs[idxs[0]].defPath] = base
			continue
		}
		type info struct {
			idx  int
			segs []string
		}
		infos := make([]info, len(idxs))
		maxLevels := 0
		for j, idx := range idxs {
			segs := spendParentSegments(refs[idx].rootPath)
			infos[j] = info{idx: idx, segs: segs}
			if len(segs) > maxLevels {
				maxLevels = len(segs)
			}
		}
		resolved := map[int]bool{}
		for level := 0; level < maxLevels && len(resolved) < len(infos); level++ {
			counts := map[string]int{}
			for i, inf := range infos {
				if resolved[i] || level >= len(inf.segs) {
					continue
				}
				counts[inf.segs[level]]++
			}
			for i, inf := range infos {
				if resolved[i] || level >= len(inf.segs) {
					continue
				}
				if counts[inf.segs[level]] == 1 {
					out[refs[inf.idx].defPath] = base + " (" + inf.segs[level] + ")"
					resolved[i] = true
				}
			}
		}
		for i, inf := range infos {
			if resolved[i] {
				continue
			}
			if len(inf.segs) == 0 {
				out[refs[inf.idx].defPath] = base
				continue
			}
			disambig := inf.segs[0]
			for level := 1; level < len(inf.segs); level++ {
				candidate := strings.Join(inf.segs[:level+1], "/")
				unique := true
				for j, other := range infos {
					if i == j || resolved[j] {
						continue
					}
					otherJoin := strings.Join(other.segs[:min(level+1, len(other.segs))], "/")
					if candidate == otherJoin {
						unique = false
						break
					}
				}
				disambig = candidate
				if unique {
					break
				}
			}
			out[refs[inf.idx].defPath] = base + " (" + disambig + ")"
		}
	}
	return out
}

func spendParentSegments(rootPath string) []string {
	parent := filepath.Dir(rootPath)
	var segs []string
	for parent != "" && parent != "." && parent != string(filepath.Separator) {
		base := filepath.Base(parent)
		if base == "" || base == "." || base == string(filepath.Separator) {
			break
		}
		segs = append(segs, base)
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return segs
}

// normalizeSpendSort maps the caller's --sort value onto the closed vocabulary.
// Empty means recency. An unrecognised value is refused with the accepted list
// and nothing is rendered.
func normalizeSpendSort(raw string) (string, error) {
	accepted := []string{SpendSortRecency, SpendSortTokens, SpendSortCost}
	switch raw {
	case "", SpendSortRecency:
		return SpendSortRecency, nil
	case SpendSortTokens:
		return SpendSortTokens, nil
	case SpendSortCost:
		return SpendSortCost, nil
	default:
		return "", exitErr(ExitSetup, "unknown spend sort %q (want one of: %s)", raw, strings.Join(accepted, ", "))
	}
}

func sortSpendRollupRows(rows []SpendRollupRow, mode string) {
	sort.SliceStable(rows, func(i, j int) bool {
		return spendRollupRowLess(rows[i], rows[j], mode)
	})
}

// spendRollupRowLess reports whether a should sort before b. Recency is every
// sort's tie-break: newer Captured-run start first, and a set with no readable
// start time always sorts after one that has one. Under cost, rate-blind rows
// form a trailing block so an unpriceable set never ranks cheapest (ADR-0218).
func spendRollupRowLess(a, b SpendRollupRow, mode string) bool {
	switch mode {
	case SpendSortTokens:
		ta, tb := tokenUsageTotal(a.Tokens), tokenUsageTotal(b.Tokens)
		if ta != tb {
			return ta > tb
		}
	case SpendSortCost:
		aBlind, bBlind := !a.Notional.HasCost, !b.Notional.HasCost
		if aBlind != bBlind {
			return !aBlind
		}
		if !aBlind && a.Notional.Dollars != b.Notional.Dollars {
			return a.Notional.Dollars > b.Notional.Dollars
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

	table := loadRateTableForSpend(d)
	result, err := buildSpendSetBreakdown(d, taskSetID, m, table)
	if err != nil {
		return nil, exitErr(ExitOperational, "spend for %s: %v", taskSetID, err)
	}
	result.RateTableDate = table.FetchedAt
	return result, nil
}

func buildSpendSetBreakdown(d *Deps, taskSetID string, m *Manifest, table *RateTable) (*SpendSetBreakdownResult, error) {
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
		notional := priceRunSpend(run, spend, table)
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
		mergePricedSpend(&row.Notional, &row.RateSource, &row.ModelKey, notional)
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
			addPartialCost(&result.VerificationNotional, notional.Cost)
		case spendReviewRowKey:
			result.ReviewRunCount++
			if !spend.Tokens.HasUsage() {
				result.ReviewTokenBlindRuns++
			}
			addTokenUsage(&result.ReviewTokens, spend.Tokens)
			addPartialCost(&result.ReviewCost, spend.Cost)
			addPartialCost(&result.ReviewNotional, notional.Cost)
		default:
			result.ImplementRunCount++
			if !spend.Tokens.HasUsage() {
				result.ImplementTokenBlindRuns++
			}
			addTokenUsage(&result.ImplementTokens, spend.Tokens)
			addPartialCost(&result.ImplementCost, spend.Cost)
			addPartialCost(&result.ImplementNotional, notional.Cost)
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

func taskSetSpendRollup(d *Deps, taskSetID, taskSetDir string, table *RateTable) (SpendRollupRow, error) {
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
		notional := priceRunSpend(run, spend, table)
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
		mergePricedSpend(&row.Notional, &row.RateSource, &row.ModelKey, notional)
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

// mergePricedSpend folds one run's priced annotation into a row's Notional,
// RateSource and ModelKey. Override wins over table; a mixed ModelKey clears
// to empty so a consumer never reads a single key as the whole row.
func mergePricedSpend(notional *PartialCost, rateSource, modelKey *string, p PricedSpend) {
	addPartialCost(notional, p.Cost)
	switch {
	case p.RateSource == RateSourceOverride:
		*rateSource = RateSourceOverride
	case p.RateSource == RateSourceTable && *rateSource == "":
		*rateSource = RateSourceTable
	}
	if p.ModelKey == "" {
		return
	}
	if *modelKey == "" {
		*modelKey = p.ModelKey
		return
	}
	if *modelKey != p.ModelKey {
		*modelKey = ""
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
	showAgents := result != nil && result.ShowAgents
	writeSpendRollupHeader(w, showAgents)
	labelBlind := result != nil && result.Sort == SpendSortCost
	blindBlock := false
	for _, row := range result.Sets {
		if labelBlind && !row.Notional.HasCost {
			if !blindBlock {
				fmt.Fprintln(w, "(rate-blind)")
				blindBlock = true
			}
		}
		writeSpendRollupRow(w, row, showAgents)
	}
	writeSpendRollupFooter(w, result)
}

func writeSpendRollupHeader(w io.Writer, showAgents bool) {
	if showAgents {
		fmt.Fprintf(w, "%-20s %-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s %18s\n",
			"project", "task set", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "tokens")
		return
	}
	fmt.Fprintf(w, "%-20s %-28s %6s %8s %8s %8s %9s %9s %5s %6s %18s\n",
		"project", "task set", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "tokens")
}

func writeSpendRollupRow(w io.Writer, row SpendRollupRow, showAgents bool) {
	agent := ""
	if showAgents {
		agent = row.Agents
	}
	cell := formatSpendCell(row.Tokens, row.Notional)
	if showAgents {
		fmt.Fprintf(w, "%-20s %-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d %18s\n",
			row.Project,
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
			cell,
		)
		return
	}
	fmt.Fprintf(w, "%-20s %-28s %6s %8s %8s %8s %9s %9s %5d %6d %18s\n",
		row.Project,
		row.TaskSetID,
		formatSpendTurns(row.Turns),
		formatSpendPeakInput(row.PeakInput),
		formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
		formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
		formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
		formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
		row.RunCount,
		row.TokenBlindRuns,
		cell,
	)
}

func writeSpendRollupFooter(w io.Writer, result *SpendRollupResult) {
	if result == nil {
		return
	}
	blind := 0
	for _, row := range result.Sets {
		blind += row.TokenBlindRuns
	}
	parts := make([]string, 0, 3)
	if blind > 0 {
		parts = append(parts, fmt.Sprintf("%d token-blind runs", blind))
	}
	if result.SkippedStorages > 0 {
		parts = append(parts, fmt.Sprintf("%d missing storages skipped", result.SkippedStorages))
	}
	if !result.RateTableDate.IsZero() {
		parts = append(parts, fmt.Sprintf("rate table %s", result.RateTableDate.UTC().Format("2006-01-02")))
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintln(w, strings.Join(parts, ", "))
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
	implementLine := fmt.Sprintf("  (%s implement", formatSpendCell(result.ImplementTokens, result.ImplementNotional))
	fmt.Fprintf(w, "%s, %d done, %d runs, %d blind)\n",
		implementLine,
		result.CompletedTasks,
		result.ImplementRunCount,
		result.ImplementTokenBlindRuns,
	)
	fmt.Fprintf(w, "verification spend: %s  (%d runs, %d blind)\n",
		formatSpendCell(result.VerificationTokens, result.VerificationNotional),
		result.VerificationRunCount,
		result.VerificationTokenBlindRuns,
	)
	// A set nobody reviewed says nothing about review: the line appears the moment
	// there is spend behind it and never before.
	if result.ReviewRunCount > 0 {
		fmt.Fprintf(w, "review spend: %s  (%d runs, %d blind)\n",
			formatSpendCell(result.ReviewTokens, result.ReviewNotional),
			result.ReviewRunCount,
			result.ReviewTokenBlindRuns,
		)
	}
	fmt.Fprintln(w)

	showAgents := result != nil && result.ShowAgents
	writeSpendBreakdownHeader(w, showAgents)
	for _, row := range result.Rows {
		writeSpendBreakdownRow(w, row, showAgents)
	}
	if result != nil && !result.RateTableDate.IsZero() {
		fmt.Fprintf(w, "rate table %s\n", result.RateTableDate.UTC().Format("2006-01-02"))
	}
}

func writeSpendBreakdownHeader(w io.Writer, showAgents bool) {
	if showAgents {
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5s %6s %18s\n",
			"task", "agent", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "tokens")
		return
	}
	fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5s %6s %18s\n",
		"task", "turns", "peak-in", "in", "out", "cache-r", "cache-w", "runs", "blind", "tokens")
}

func writeSpendBreakdownRow(w io.Writer, row SpendBreakdownRow, showAgents bool) {
	agent := ""
	if showAgents {
		agent = row.Agent
	}
	cell := formatSpendCell(row.Tokens, row.Notional)
	if showAgents {
		fmt.Fprintf(w, "%-28s %6s %6s %8s %8s %8s %9s %9s %5d %6d %18s\n",
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
			cell,
		)
		return
	}
	fmt.Fprintf(w, "%-28s %6s %8s %8s %8s %9s %9s %5d %6d %18s\n",
		row.TaskID,
		formatSpendTurns(row.Turns),
		formatSpendPeakInput(row.PeakInput),
		formatSpendCount(row.Tokens.HasInput, row.Tokens.Input),
		formatSpendCount(row.Tokens.HasOutput, row.Tokens.Output),
		formatSpendCount(row.Tokens.HasCacheRead, row.Tokens.CacheRead),
		formatSpendCount(row.Tokens.HasCacheWrite, row.Tokens.CacheWrite),
		row.RunCount,
		row.TokenBlindRuns,
		cell,
	)
}

func formatSpendPerCompletedTask(result *SpendSetBreakdownResult) string {
	if result.TokensPerCompletedTask == nil {
		return "—"
	}
	tokens := fmt.Sprintf("%d", *result.TokensPerCompletedTask)
	if result.ImplementNotional.HasCost && result.CompletedTasks > 0 {
		return fmt.Sprintf("%s (~$%.2f)", tokens, result.ImplementNotional.Dollars/float64(result.CompletedTasks))
	}
	return fmt.Sprintf("%s (—)", tokens)
}

// formatSpendCell is the shared tokens (~$notional) cell for rollup and
// breakdown surfaces. Rate-blind figures render (—), never (~$0.00).
func formatSpendCell(u TokenUsage, notional PartialCost) string {
	if !u.HasUsage() {
		return "—"
	}
	tokens := fmt.Sprintf("%d", tokenUsageTotal(u))
	if notional.HasCost {
		return fmt.Sprintf("%s (~$%.2f)", tokens, notional.Dollars)
	}
	return fmt.Sprintf("%s (—)", tokens)
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

func rateSourceJSONPtr(source string) *string {
	if source == "" {
		return nil
	}
	v := source
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
			NotionalCostUSD:  partialCostUSDPtr(row.Notional),
			RateSource:       rateSourceJSONPtr(row.RateSource),
			ModelKey:         row.ModelKey,
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
			Project:          row.Project,
			InputTokens:      row.Tokens.Input,
			OutputTokens:     row.Tokens.Output,
			CacheReadTokens:  row.Tokens.CacheRead,
			CacheWriteTokens: row.Tokens.CacheWrite,
			PartialCostUSD:   partialCostUSDPtr(row.Cost),
			NotionalCostUSD:  partialCostUSDPtr(row.Notional),
			RateSource:       rateSourceJSONPtr(row.RateSource),
			ModelKey:         row.ModelKey,
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
