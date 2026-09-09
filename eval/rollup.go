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
	"sort"
)

type metricRollup struct {
	Median *float64 `json:"median"`
	Spread *float64 `json:"spread"`
	Blind  int      `json:"blind"`
}

type evalRollupRow struct {
	Case             string       `json:"case"`
	Arm              string       `json:"arm"`
	Trials           int          `json:"trials"`
	Counted          int          `json:"counted"`
	TotalTokens      metricRollup `json:"total_tokens"`
	NotionalCostUSD  metricRollup `json:"notional_cost_usd"`
	Turns            metricRollup `json:"turns"`
	PeakInputTokens  metricRollup `json:"peak_input_tokens"`
	WallClockSeconds metricRollup `json:"wall_clock_seconds"`
	AcceptanceRatio  *float64     `json:"acceptance_ratio"`
	QualityScore     *float64     `json:"quality_score"`
	GateFailures     int          `json:"gate_failures"`
	Timeouts         int          `json:"timeouts"`
	Invalid          int          `json:"invalid"`
	Lost             int          `json:"lost"`
	Ungraded         int          `json:"ungraded"`
}

type evalRollup struct {
	Rows []evalRollupRow `json:"rows"`
}

type rollupAccumulator struct {
	row                                             evalRollupRow
	tokens, cost, turns, peak, wall, ratio, quality []float64
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
	jsonOutput := flags.Bool("json", false, "Emit the Rollup as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: go run ./eval rollup [--results <dir>] [--json]")
	}
	rollup, err := loadEvalRollup(*results)
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

func loadEvalRollup(root string) (evalRollup, error) {
	groups := map[string]*rollupAccumulator{}
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
		key := record.Case + "\x00" + record.Arm
		group := groups[key]
		if group == nil {
			group = &rollupAccumulator{row: evalRollupRow{Case: record.Case, Arm: record.Arm}}
			groups[key] = group
		}
		return group.add(record)
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

func (a *rollupAccumulator) add(record trialRecord) error {
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
		a.quality = append(a.quality, float64(record.Grade.Scores.Quality))
	}
	return nil
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
	tokenBlind := spend.ImplementTokenBlindRuns + spend.VerificationTokenBlindRuns + spend.RefineTokenBlindRuns
	runs := spend.ImplementRunCount + spend.VerificationRunCount + spend.RefineRunCount
	if runs > 0 && tokenBlind == 0 {
		value := float64(spend.ImplementInputTokens + spend.ImplementOutputTokens + spend.ImplementCacheReadTokens + spend.ImplementCacheWriteTokens +
			spend.VerificationInputTokens + spend.VerificationOutputTokens + spend.VerificationCacheReadTokens + spend.VerificationCacheWriteTokens +
			spend.RefineInputTokens + spend.RefineOutputTokens + spend.RefineCacheReadTokens + spend.RefineCacheWriteTokens)
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
	fmt.Fprintf(w, "%-24s %-16s %6s %17s %17s %17s %17s %17s %8s %7s %4s %4s %4s %4s %4s %s\n",
		"case", "arm", "trials", "tokens median/spread", "cost median/spread", "turns median/spread", "peak median/spread", "wall-s median/spread", "accept", "quality", "gate", "time", "inv", "lost", "ungr", "blind tok/$/turn/peak/wall")
	for _, row := range rollup.Rows {
		fmt.Fprintf(w, "%-24s %-16s %6d %17s %17s %17s %17s %17s %8s %7s %4d %4d %4d %4d %4d %d/%d/%d/%d/%d\n",
			row.Case, row.Arm, row.Trials,
			formatMetric(row.TotalTokens, 0), formatMetric(row.NotionalCostUSD, 4), formatMetric(row.Turns, 1), formatMetric(row.PeakInputTokens, 0), formatMetric(row.WallClockSeconds, 1),
			formatOptional(row.AcceptanceRatio, 3), formatOptional(row.QualityScore, 1), row.GateFailures, row.Timeouts, row.Invalid, row.Lost, row.Ungraded,
			row.TotalTokens.Blind, row.NotionalCostUSD.Blind, row.Turns.Blind, row.PeakInputTokens.Blind, row.WallClockSeconds.Blind)
	}
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
