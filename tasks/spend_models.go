package tasks

import (
	"sort"
	"strings"
)

// spendModelBlindKey is the map key and display label for runs whose model
// could not be resolved (ADR-0218).
const spendModelBlindKey = "—"

// spendModelSplit is the per-model token accumulator behind one spend row.
type spendModelSplit map[string]TokenUsage

func (split spendModelSplit) record(key string, tokens TokenUsage) {
	if split == nil {
		return
	}
	usage := split[key]
	addTokenUsage(&usage, tokens)
	split[key] = usage
}

func resolveSpendModelKey(run capturedRun, table *RateTable) string {
	key := resolveRunRateKey(run, table).Key
	if key == "" {
		return spendModelBlindKey
	}
	return key
}

// formatSpendModels renders the human model cell for one row. A single readable
// model names itself; several readable models name the dominant with a mix
// marker; all-unreadable models render the blind marker (ADR-0218).
func formatSpendModels(split spendModelSplit) string {
	if len(split) == 0 {
		return ""
	}
	readable := spendReadableModelKeys(split)
	if len(readable) == 0 {
		return spendModelBlindKey
	}
	if len(readable) == 1 && len(split) == 1 {
		return readable[0]
	}
	return dominantSpendModel(split, readable) + "+"
}

func spendReadableModelKeys(split spendModelSplit) []string {
	keys := make([]string, 0, len(split))
	for key := range split {
		if key != spendModelBlindKey {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func dominantSpendModel(split spendModelSplit, keys []string) string {
	best := keys[0]
	bestTotal := tokenUsageTotal(split[best])
	for _, key := range keys[1:] {
		if total := tokenUsageTotal(split[key]); total > bestTotal {
			best = key
			bestTotal = total
		}
	}
	return best
}

func spendModelsMix(models string) bool {
	return strings.HasSuffix(models, "+")
}

func recordSpendRowModel(row *SpendBreakdownRow, key string, tokens TokenUsage) {
	if row.ModelSplit == nil {
		row.ModelSplit = spendModelSplit{}
	}
	row.ModelSplit.record(key, tokens)
	row.Models = formatSpendModels(row.ModelSplit)
}
