package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

type metricRollup struct {
	Median *float64 `json:"median"`
	Spread *float64 `json:"spread"`
	Blind  int      `json:"blind"`
}

type evalRollupRow struct {
	Case string `json:"case"`
	Arm  string `json:"arm"`
	// PopVersion splits a Pop-kind Arm's Trials by the Pop revision they ran,
	// so medians never mix two revisions. Empty for Bare-kind Arms and for
	// Trials recorded before the field existed.
	PopVersion       string       `json:"pop_version"`
	Trials           int          `json:"trials"`
	Counted          int          `json:"counted"`
	TotalTokens      metricRollup `json:"total_tokens"`
	NotionalCostUSD  metricRollup `json:"notional_cost_usd"`
	Turns            metricRollup `json:"turns"`
	PeakInputTokens  metricRollup `json:"peak_input_tokens"`
	WallClockSeconds metricRollup `json:"wall_clock_seconds"`
	AcceptanceRatio  *float64     `json:"acceptance_ratio"`
	// NameFreeAcceptanceRatio leaves out the Case's name-only items, so an Arm
	// that never saw the planned names is not scored on them.
	NameFreeAcceptanceRatio *float64 `json:"name_free_acceptance_ratio"`
	QualityScore            *float64 `json:"quality_score"`
	// SplitItems is the most items in one Trial's grade on which the Grader
	// samples did not agree: how far a regrade could move that Trial.
	SplitItems       int `json:"split_items"`
	GateFailures     int `json:"gate_failures"`
	BaselineFailures int `json:"baseline_failures"`
	Timeouts         int `json:"timeouts"`
	Invalid          int `json:"invalid"`
	Lost             int `json:"lost"`
	Ungraded         int `json:"ungraded"`
	// Stale counts Trials whose recorded origin differs from the Case and the
	// harness now, and graded Trials whose Grader read another version of the
	// Case's Acceptance list than the one the Case holds now.
	Stale int `json:"stale"`
	// Unknown counts Trials recorded before Trials named an origin, so nothing
	// says whether that origin is current.
	Unknown int `json:"unknown"`
}

type evalRollup struct {
	Rows []evalRollupRow `json:"rows"`
}

type rollupAccumulator struct {
	row                                                       evalRollupRow
	tokens, cost, turns, peak, wall, ratio, nameFree, quality []float64
}

// currentCase is what the Rollup needs from a Case as it is now: its parent
// commit and its Acceptance list.
type currentCase struct {
	parent   string
	digest   string
	nameOnly map[int]bool
}

type popSpendJSON struct {
	ImplementInputTokens         int64    `json:"implement_input_tokens"`
	ImplementOutputTokens        int64    `json:"implement_output_tokens"`
	ImplementCacheReadTokens     int64    `json:"implement_cache_read_tokens"`
	ImplementCacheWriteTokens    int64    `json:"implement_cache_write_tokens"`
	ImplementRunCount            int      `json:"implement_run_count"`
	ImplementTokenBlindRuns      int      `json:"implement_token_blind_runs"`
	ImplementNotionalCostUSD     *float64 `json:"implement_notional_cost_usd"`
	VerificationInputTokens      int64    `json:"verification_input_tokens"`
	VerificationOutputTokens     int64    `json:"verification_output_tokens"`
	VerificationCacheReadTokens  int64    `json:"verification_cache_read_tokens"`
	VerificationCacheWriteTokens int64    `json:"verification_cache_write_tokens"`
	VerificationRunCount         int      `json:"verification_run_count"`
	VerificationTokenBlindRuns   int      `json:"verification_token_blind_runs"`
	VerificationNotionalCostUSD  *float64 `json:"verification_notional_cost_usd"`
	RefineInputTokens            int64    `json:"refine_input_tokens"`
	RefineOutputTokens           int64    `json:"refine_output_tokens"`
	RefineCacheReadTokens        int64    `json:"refine_cache_read_tokens"`
	RefineCacheWriteTokens       int64    `json:"refine_cache_write_tokens"`
	RefineRunCount               int      `json:"refine_run_count"`
	RefineTokenBlindRuns         int      `json:"refine_token_blind_runs"`
	RefineNotionalCostUSD        *float64 `json:"refine_notional_cost_usd"`
	ExploreInputTokens           int64    `json:"explore_input_tokens"`
	ExploreOutputTokens          int64    `json:"explore_output_tokens"`
	ExploreCacheReadTokens       int64    `json:"explore_cache_read_tokens"`
	ExploreCacheWriteTokens      int64    `json:"explore_cache_write_tokens"`
	ExploreRunCount              int      `json:"explore_run_count"`
	ExploreTokenBlindRuns        int      `json:"explore_token_blind_runs"`
	ExploreNotionalCostUSD       *float64 `json:"explore_notional_cost_usd"`
	Rows                         []struct {
		Turns           *int   `json:"turns"`
		TurnBlindRuns   int    `json:"turn_blind_runs"`
		PeakInputTokens *int64 `json:"peak_input_tokens"`
		PeakBlindRuns   int    `json:"peak_blind_runs"`
	} `json:"rows"`
}

func runRollupCommand(args []string) error {
	flags := flag.NewFlagSet("rollup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	results := flags.String("results", defaultResultsRoot, "Trial record directory root")
	cases := flags.String("cases", defaultCasesRoot, "Case directory root")
	jsonOutput := flags.Bool("json", false, "Emit the Rollup as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: go run ./eval rollup [--results <dir>] [--cases <dir>] [--json]")
	}
	rollup, err := loadEvalRollup(*results, *cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return renderEvalRollupJSON(os.Stdout, rollup)
	}
	renderEvalRollup(os.Stdout, rollup)
	return nil
}

func renderEvalRollupJSON(w io.Writer, rollup evalRollup) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rollup)
}

// loadEvalRollup groups every Trial record under root by Case, Arm and Pop
// revision. cases is read only for each Case's current parent commit and
// Acceptance list, so a Trial run from an older parent or another agent home,
// or a grade made with an older list or another Grader home, is counted as
// stale.
func loadEvalRollup(root, cases string) (evalRollup, error) {
	groups := map[string]*rollupAccumulator{}
	currentCases := map[string]currentCase{}
	homeDigest := agentHomeDigest()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "trial.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record trialRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("read Trial record %s: %w", path, err)
		}
		if record.Case == "" || record.Arm == "" || record.Repeat < 1 {
			return fmt.Errorf("read Trial record %s: incomplete identity", path)
		}
		key := record.Case + "\x00" + record.Arm + "\x00" + record.PopVersion
		group := groups[key]
		if group == nil {
			group = &rollupAccumulator{row: evalRollupRow{Case: record.Case, Arm: record.Arm, PopVersion: record.PopVersion}}
			groups[key] = group
		}
		current, known := currentCases[record.Case]
		if !known {
			caseDir := filepath.Join(cases, record.Case)
			if manifest, err := readCaseManifest(caseDir); err == nil {
				current.parent = manifest.ParentCommit
			}
			if data, err := os.ReadFile(filepath.Join(caseDir, acceptanceName)); err == nil {
				current.digest = acceptanceDigest(string(data))
				if current.nameOnly, err = nameOnlyItems(string(data)); err != nil {
					return fmt.Errorf("read Acceptance list of Case %s: %w", record.Case, err)
				}
			}
			currentCases[record.Case] = current
		}
		origins := []origin{
			compareOrigin(record.ParentCommit, current.parent),
			compareOrigin(record.AgentHomeDigest, homeDigest),
		}
		graded := record.Grade != nil && record.Grade.Status == "graded"
		if graded {
			origins = append(origins, compareOrigin(record.Grade.AgentHomeDigest, homeDigest))
		}
		staleList := graded && (current.digest == "" || record.Grade.AcceptanceDigest != current.digest)
		if slices.Contains(origins, originStale) || staleList {
			group.row.Stale++
		}
		if slices.Contains(origins, originUnknown) {
			group.row.Unknown++
		}
		return group.add(record, current.nameOnly)
	})
	if err != nil {
		if os.IsNotExist(err) {
			return evalRollup{Rows: []evalRollupRow{}}, nil
		}
		return evalRollup{}, err
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := evalRollup{Rows: make([]evalRollupRow, 0, len(keys))}
	for _, key := range keys {
		group := groups[key]
		group.finish()
		result.Rows = append(result.Rows, group.row)
	}
	return result, nil
}

// origin is how one thing a Trial recorded about where it ran from compares
// with the Case and the harness now: its parent commit, its agent home, and
// its Grader's agent home.
type origin int

const (
	originCurrent origin = iota
	originStale
	originUnknown
)

// compareOrigin is the one rule for each recorded origin. A record from before
// the field existed says nothing, so it is unknown rather than stale.
func compareOrigin(recorded, current string) origin {
	switch {
	case recorded == "":
		return originUnknown
	case recorded != current:
		return originStale
	default:
		return originCurrent
	}
}

func (a *rollupAccumulator) add(record trialRecord, nameOnly map[int]bool) error {
	a.row.Trials++
	if record.Outcome == outcomeInvalid {
		a.row.Invalid++
	}
	if record.Outcome == outcomeLost {
		a.row.Lost++
	}
	if record.Outcome == outcomeTimedOut {
		a.row.Timeouts++
	}
	if record.Grade == nil || record.Grade.Status == "ungraded" {
		a.row.Ungraded++
	}
	if record.Grade != nil && record.Grade.Status == "gate_failed" {
		a.row.GateFailures++
	}
	if record.Grade != nil && record.Grade.Status == "baseline_failed" {
		a.row.BaselineFailures++
	}
	if record.Outcome == outcomeInvalid {
		return nil
	}
	a.row.Counted++
	figures, err := trialFigures(record)
	if err != nil {
		return err
	}
	addFigure(&a.tokens, &a.row.TotalTokens, figures.tokens)
	addFigure(&a.cost, &a.row.NotionalCostUSD, figures.cost)
	addFigure(&a.turns, &a.row.Turns, figures.turns)
	addFigure(&a.peak, &a.row.PeakInputTokens, figures.peak)
	wall := record.EndedAt.Sub(record.StartedAt).Seconds()
	if record.StartedAt.IsZero() || record.EndedAt.Before(record.StartedAt) {
		addFigure(&a.wall, &a.row.WallClockSeconds, nil)
	} else {
		addFigure(&a.wall, &a.row.WallClockSeconds, &wall)
	}
	if record.Outcome != outcomeLost && record.Grade != nil && record.Grade.Scores != nil {
		a.ratio = append(a.ratio, record.Grade.Scores.ListRatio)
		a.nameFree = append(a.nameFree, nameFreeRatio(*record.Grade.Scores, nameOnly))
		a.quality = append(a.quality, float64(record.Grade.Scores.Quality))
		a.row.SplitItems = max(a.row.SplitItems, splitItems(*record.Grade.Scores))
	}
	return nil
}

// nameFreeRatio is the list ratio over the items that are not name-only. A
// grade with no items, such as a failed gate, keeps its list ratio of zero.
func nameFreeRatio(scores gradeScores, nameOnly map[int]bool) float64 {
	counted, met := 0, 0
	for _, item := range scores.Items {
		if nameOnly[item.Item] {
			continue
		}
		counted++
		if item.Met {
			met++
		}
	}
	if counted == 0 {
		return scores.ListRatio
	}
	return float64(met) / float64(counted)
}

type optionalFigures struct {
	tokens, cost, turns, peak *float64
}

func trialFigures(record trialRecord) (optionalFigures, error) {
	if len(record.SetSpend) == 0 {
		var result optionalFigures
		if record.Spend.Tokens.HasUsage() {
			value := float64(record.Spend.Tokens.Input + record.Spend.Tokens.Output + record.Spend.Tokens.CacheRead + record.Spend.Tokens.CacheWrite)
			result.tokens = &value
		}
		if record.Notional.Cost.HasCost {
			value := record.Notional.Cost.Dollars
			result.cost = &value
		}
		if record.Spend.Turns.HasTurn {
			value := float64(record.Spend.Turns.Count)
			result.turns = &value
		}
		if record.Spend.PeakInput.HasPeak {
			value := float64(record.Spend.PeakInput.Tokens)
			result.peak = &value
		}
		return result, nil
	}
	var spend popSpendJSON
	if err := json.Unmarshal(record.SetSpend, &spend); err != nil {
		return optionalFigures{}, fmt.Errorf("read Pop-arm spend for %s/%s/%d: %w", record.Case, record.Arm, record.Repeat, err)
	}
	var result optionalFigures
	tokenBlind := spend.ImplementTokenBlindRuns + spend.VerificationTokenBlindRuns + spend.RefineTokenBlindRuns + spend.ExploreTokenBlindRuns
	runs := spend.ImplementRunCount + spend.VerificationRunCount + spend.RefineRunCount + spend.ExploreRunCount
	if runs > 0 && tokenBlind == 0 {
		value := float64(spend.ImplementInputTokens + spend.ImplementOutputTokens + spend.ImplementCacheReadTokens + spend.ImplementCacheWriteTokens +
			spend.VerificationInputTokens + spend.VerificationOutputTokens + spend.VerificationCacheReadTokens + spend.VerificationCacheWriteTokens +
			spend.RefineInputTokens + spend.RefineOutputTokens + spend.RefineCacheReadTokens + spend.RefineCacheWriteTokens +
			spend.ExploreInputTokens + spend.ExploreOutputTokens + spend.ExploreCacheReadTokens + spend.ExploreCacheWriteTokens)
		result.tokens = &value
	}
	var cost float64
	turns, peak := 0, int64(0)
	costKnown, turnsKnown, peakKnown := runs > 0, len(spend.Rows) > 0, len(spend.Rows) > 0
	for _, phase := range []struct {
		runs int
		cost *float64
	}{
		{spend.ImplementRunCount, spend.ImplementNotionalCostUSD},
		{spend.VerificationRunCount, spend.VerificationNotionalCostUSD},
		{spend.RefineRunCount, spend.RefineNotionalCostUSD},
		{spend.ExploreRunCount, spend.ExploreNotionalCostUSD},
	} {
		if phase.runs == 0 {
			continue
		}
		if phase.cost == nil {
			costKnown = false
		} else {
			cost += *phase.cost
		}
	}
	for _, row := range spend.Rows {
		if row.Turns == nil || row.TurnBlindRuns > 0 {
			turnsKnown = false
		} else {
			turns += *row.Turns
		}
		if row.PeakInputTokens == nil || row.PeakBlindRuns > 0 {
			peakKnown = false
		} else if *row.PeakInputTokens > peak {
			peak = *row.PeakInputTokens
		}
	}
	if costKnown {
		result.cost = &cost
	}
	if turnsKnown {
		value := float64(turns)
		result.turns = &value
	}
	if peakKnown {
		value := float64(peak)
		result.peak = &value
	}
	return result, nil
}

func addFigure(values *[]float64, rollup *metricRollup, value *float64) {
	if value == nil {
		rollup.Blind++
		return
	}
	*values = append(*values, *value)
}

func (a *rollupAccumulator) finish() {
	a.row.TotalTokens = summarize(a.tokens, a.row.TotalTokens.Blind)
	a.row.NotionalCostUSD = summarize(a.cost, a.row.NotionalCostUSD.Blind)
	a.row.Turns = summarize(a.turns, a.row.Turns.Blind)
	a.row.PeakInputTokens = summarize(a.peak, a.row.PeakInputTokens.Blind)
	a.row.WallClockSeconds = summarize(a.wall, a.row.WallClockSeconds.Blind)
	a.row.AcceptanceRatio = median(a.ratio)
	a.row.NameFreeAcceptanceRatio = median(a.nameFree)
	a.row.QualityScore = median(a.quality)
}

func summarize(values []float64, blind int) metricRollup {
	if len(values) == 0 {
		return metricRollup{Blind: blind}
	}
	sort.Float64s(values)
	medianValue := medianSorted(values)
	spread := values[len(values)-1] - values[0]
	return metricRollup{Median: &medianValue, Spread: &spread, Blind: blind}
}

func median(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	value := medianSorted(sorted)
	return &value
}

func medianSorted(values []float64) float64 {
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func renderEvalRollup(w io.Writer, rollup evalRollup) {
	fmt.Fprintf(w, "%-24s %-16s %6s %17s %17s %17s %17s %17s %8s %9s %7s %5s %4s %4s %4s %4s %4s %4s %5s %4s %-26s %s\n",
		"case", "arm", "trials", "tokens median/spread", "cost median/spread", "turns median/spread", "peak median/spread", "wall-s median/spread", "accept", "accept-nn", "quality", "split", "gate", "base", "time", "inv", "lost", "ungr", "stale", "unkn", "blind tok/$/turn/peak/wall", "pop")
	for _, row := range rollup.Rows {
		fmt.Fprintf(w, "%-24s %-16s %6d %17s %17s %17s %17s %17s %8s %9s %7s %5d %4d %4d %4d %4d %4d %4d %5d %4d %-26s %s\n",
			row.Case, row.Arm, row.Trials,
			formatMetric(row.TotalTokens, 0), formatMetric(row.NotionalCostUSD, 4), formatMetric(row.Turns, 1), formatMetric(row.PeakInputTokens, 0), formatMetric(row.WallClockSeconds, 1),
			formatOptional(row.AcceptanceRatio, 3), formatOptional(row.NameFreeAcceptanceRatio, 3), formatOptional(row.QualityScore, 1), row.SplitItems, row.GateFailures, row.BaselineFailures, row.Timeouts, row.Invalid, row.Lost, row.Ungraded, row.Stale, row.Unknown,
			fmt.Sprintf("%d/%d/%d/%d/%d", row.TotalTokens.Blind, row.NotionalCostUSD.Blind, row.Turns.Blind, row.PeakInputTokens.Blind, row.WallClockSeconds.Blind),
			formatPopVersion(row.PopVersion))
	}
}

func formatPopVersion(version string) string {
	if version == "" {
		return "—"
	}
	return version
}

func formatMetric(metric metricRollup, precision int) string {
	if metric.Median == nil || metric.Spread == nil {
		return "—"
	}
	return fmt.Sprintf("%.*f/%.*f", precision, *metric.Median, precision, *metric.Spread)
}

func formatOptional(value *float64, precision int) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.*f", precision, *value)
}
