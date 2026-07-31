package tasks

import "fmt"

// OverCountGuardStatus reports whether the usage over-count guard applied to a
// run. Adapters with no independent terminal total are Inapplicable rather
// than silently passing (ADR-0160).
type OverCountGuardStatus int

const (
	OverCountGuardInapplicable OverCountGuardStatus = iota
	OverCountGuardOK
)

// terminalTokenUsageReaders map agent preset → independent terminal-total
// reader. These are separate from Usage extraction rules so a mistaken
// accumulate/replace implementation is caught when summed usage exceeds what
// the run's own terminal event reports.
var terminalTokenUsageReaders = map[string]func([]streamEventRecord) (TokenUsage, bool){
	"cursor": cursorTerminalTokenUsage,
	"pi":     piTerminalTokenUsage,
}

// checkUsageOverCountGuard verifies extracted usage does not exceed the run's
// terminal-reported total. A violation returns an error — spend must fail
// loudly rather than render a plausible wrong figure.
func checkUsageOverCountGuard(agent string, events []streamEventRecord, extracted TokenUsage) (OverCountGuardStatus, error) {
	reader := terminalTokenUsageReaders[agent]
	if reader == nil {
		return OverCountGuardInapplicable, nil
	}
	terminal, ok := reader(events)
	if !ok {
		return OverCountGuardInapplicable, nil
	}
	if field, got, ceiling, over := tokenUsageExceeds(extracted, terminal); over {
		return OverCountGuardOK, fmt.Errorf(
			"usage over-count guard: %s run %s tokens %d exceed terminal %d",
			agent, field, got, ceiling,
		)
	}
	return OverCountGuardOK, nil
}

// tokenUsageExceeds reports whether a exceeds ceiling on any field the
// terminal total reports.
func tokenUsageExceeds(a, ceiling TokenUsage) (field string, got, max int64, over bool) {
	if ceiling.HasInput && a.Input > ceiling.Input {
		return "input", a.Input, ceiling.Input, true
	}
	if ceiling.HasOutput && a.Output > ceiling.Output {
		return "output", a.Output, ceiling.Output, true
	}
	if ceiling.HasCacheRead && a.CacheRead > ceiling.CacheRead {
		return "cache-read", a.CacheRead, ceiling.CacheRead, true
	}
	if ceiling.HasCacheWrite && a.CacheWrite > ceiling.CacheWrite {
		return "cache-write", a.CacheWrite, ceiling.CacheWrite, true
	}
	return "", 0, 0, false
}
