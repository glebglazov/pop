package tasks

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// streamShapeCapability names one of the six stream-shape adapter capabilities
// gated by captured-run fixtures (ADR-0165).
type streamShapeCapability int

const (
	streamShapeUsage streamShapeCapability = iota
	streamShapeCost
	streamShapeToolTimings
	streamShapeActualModel
	streamShapeStreamRender
	streamShapeTurn
)

var streamShapeCapabilityOrder = []streamShapeCapability{
	streamShapeUsage,
	streamShapeCost,
	streamShapeToolTimings,
	streamShapeActualModel,
	streamShapeStreamRender,
	streamShapeTurn,
}

func (c streamShapeCapability) String() string {
	switch c {
	case streamShapeUsage:
		return "usage"
	case streamShapeCost:
		return "cost"
	case streamShapeToolTimings:
		return "tool-timings"
	case streamShapeActualModel:
		return "actual-model"
	case streamShapeStreamRender:
		return "stream-render"
	case streamShapeTurn:
		return "turn"
	default:
		return fmt.Sprintf("stream-shape-capability(%d)", c)
	}
}

// toolTimingGolden is one row in a tool-timings fixture golden.
type toolTimingGolden struct {
	Name       string
	Count      int
	TotalNanos int64
}

// streamRenderGolden captures a stable summary of rendered stream events.
type streamRenderGolden struct {
	EventCount int
	TypeCounts map[string]int
}

// streamShapeGolden holds expected extraction output for one capability on a
// preset's trimmed captured stream. A nil pointer means no golden is registered.
type streamShapeGolden struct {
	usage        *TokenUsage
	cost         *PartialCost
	toolTimings  []toolTimingGolden
	actualModel  *string
	streamRender *streamRenderGolden
	turn         *TurnCount
}

func (g *streamShapeGolden) hasGolden(cap streamShapeCapability) bool {
	if g == nil {
		return false
	}
	switch cap {
	case streamShapeUsage:
		return g.usage != nil
	case streamShapeCost:
		return g.cost != nil
	case streamShapeToolTimings:
		return g.toolTimings != nil
	case streamShapeActualModel:
		return g.actualModel != nil
	case streamShapeStreamRender:
		return g.streamRender != nil
	case streamShapeTurn:
		return g.turn != nil
	default:
		return false
	}
}

// streamFixturePath returns the trimmed captured stream for a preset.
func streamFixturePath(preset string) string {
	return filepath.Join("testdata", "streams", preset+".events.jsonl.gz")
}

func streamFixtureExists(preset string) bool {
	_, err := os.Stat(streamFixturePath(preset))
	return err == nil
}

func loadStreamFixture(preset string) ([]streamEventRecord, error) {
	path := streamFixturePath(preset)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	dec := json.NewDecoder(zr)
	var events []streamEventRecord
	for dec.More() {
		var ev streamEventRecord
		if err := dec.Decode(&ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func streamShapeCapabilityKind(adapter AgentAdapter, cap streamShapeCapability) CapabilityKind {
	switch cap {
	case streamShapeUsage:
		return adapter.UsageCapability().Kind
	case streamShapeCost:
		return adapter.CostCapability().Kind
	case streamShapeToolTimings:
		return adapter.ToolTimingCapability().Kind
	case streamShapeActualModel:
		return adapter.ActualModelCapability().Kind
	case streamShapeStreamRender:
		return adapter.StreamRenderCapability().Kind
	case streamShapeTurn:
		return adapter.TurnCapability().Kind
	default:
		return capabilityUnset
	}
}

func extractStreamShapeOutput(preset string, cap streamShapeCapability, events []streamEventRecord) (any, error) {
	switch cap {
	case streamShapeUsage:
		return extractTokenUsage(preset, events), nil
	case streamShapeCost:
		return extractPartialCost(preset, events), nil
	case streamShapeToolTimings:
		tools, _ := extractToolTimings(preset, events)
		return tools, nil
	case streamShapeActualModel:
		return extractActualModel(preset, events), nil
	case streamShapeStreamRender:
		return summarizeStreamRender(renderStreamEvents(preset, events)), nil
	case streamShapeTurn:
		return extractTurnCount(preset, events), nil
	default:
		return nil, fmt.Errorf("unknown stream-shape capability %v", cap)
	}
}

func summarizeStreamRender(events []StreamEvent) streamRenderGolden {
	counts := make(map[string]int)
	for _, ev := range events {
		counts[ev.Type]++
	}
	return streamRenderGolden{EventCount: len(events), TypeCounts: counts}
}

func streamShapeGoldenMismatch(cap streamShapeCapability, want, got any) string {
	switch cap {
	case streamShapeUsage:
		return fmt.Sprintf("usage: got %+v, want %+v", got.(TokenUsage), want.(TokenUsage))
	case streamShapeCost:
		return fmt.Sprintf("cost: got %+v, want %+v", got.(PartialCost), want.(PartialCost))
	case streamShapeToolTimings:
		return fmt.Sprintf("tool-timings: got %v, want %v", formatToolTimings(got.([]ToolTiming)), formatToolTimingGoldens(want.([]toolTimingGolden)))
	case streamShapeActualModel:
		return fmt.Sprintf("actual-model: got %q, want %q", got.(string), want.(string))
	case streamShapeStreamRender:
		return fmt.Sprintf("stream-render: got %+v, want %+v", got.(streamRenderGolden), want.(streamRenderGolden))
	case streamShapeTurn:
		return fmt.Sprintf("turn: got %+v, want %+v", got.(TurnCount), want.(TurnCount))
	default:
		return fmt.Sprintf("mismatch for %s", cap)
	}
}

func formatToolTimings(tools []ToolTiming) []toolTimingGolden {
	out := make([]toolTimingGolden, len(tools))
	for i, tool := range tools {
		out[i] = toolTimingGolden{Name: tool.Name, Count: tool.Count, TotalNanos: int64(tool.Total)}
	}
	return out
}

func formatToolTimingGoldens(goldens []toolTimingGolden) []toolTimingGolden {
	return goldens
}

func toolTimingsMatch(got []ToolTiming, want []toolTimingGolden) bool {
	if len(got) != len(want) {
		return false
	}
	formatted := formatToolTimings(got)
	for i := range want {
		if formatted[i] != want[i] {
			return false
		}
	}
	return true
}

func streamRenderGoldensMatch(got, want streamRenderGolden) bool {
	if got.EventCount != want.EventCount {
		return false
	}
	if len(got.TypeCounts) != len(want.TypeCounts) {
		return false
	}
	for k, v := range want.TypeCounts {
		if got.TypeCounts[k] != v {
			return false
		}
	}
	return true
}

func streamShapeGoldenValue(g *streamShapeGolden, cap streamShapeCapability) any {
	switch cap {
	case streamShapeUsage:
		return *g.usage
	case streamShapeCost:
		return *g.cost
	case streamShapeToolTimings:
		return g.toolTimings
	case streamShapeActualModel:
		return *g.actualModel
	case streamShapeStreamRender:
		return *g.streamRender
	case streamShapeTurn:
		return *g.turn
	default:
		return nil
	}
}

func streamShapeOutputsMatch(cap streamShapeCapability, want, got any) bool {
	switch cap {
	case streamShapeUsage:
		return got.(TokenUsage) == want.(TokenUsage)
	case streamShapeCost:
		return got.(PartialCost) == want.(PartialCost)
	case streamShapeToolTimings:
		return toolTimingsMatch(got.([]ToolTiming), want.([]toolTimingGolden))
	case streamShapeActualModel:
		return got.(string) == want.(string)
	case streamShapeStreamRender:
		return streamRenderGoldensMatch(got.(streamRenderGolden), want.(streamRenderGolden))
	case streamShapeTurn:
		return got.(TurnCount) == want.(TurnCount)
	default:
		return false
	}
}

// streamShapeFixtureViolation records one harness gate failure.
type streamShapeFixtureViolation struct {
	Preset      string
	Capability  streamShapeCapability
	Description string
}

// streamShapeOutputPresent reports whether a capability's extraction output
// carries real stream-derived data (not a blind/absent result).
func streamShapeOutputPresent(cap streamShapeCapability, got any) bool {
	switch cap {
	case streamShapeUsage:
		return got.(TokenUsage).HasUsage()
	case streamShapeCost:
		return got.(PartialCost).HasCost
	case streamShapeToolTimings:
		return len(got.([]ToolTiming)) > 0
	case streamShapeActualModel:
		return got.(string) != ""
	case streamShapeStreamRender:
		g := got.(streamRenderGolden)
		if g.EventCount == 0 {
			return false
		}
		if len(g.TypeCounts) == 1 && g.TypeCounts["render_refused"] == g.EventCount {
			return false
		}
		return true
	case streamShapeTurn:
		return got.(TurnCount).HasTurn
	default:
		return false
	}
}

// checkStreamShapeFixture applies the ADR-0165 fixture gate for one preset and
// capability. fixtureExists is whether a trimmed stream file is on disk;
// goldens may be nil when no golden table entry exists for the preset.
func checkStreamShapeFixture(preset string, cap streamShapeCapability, kind CapabilityKind, fixtureExists bool, goldens *streamShapeGolden, events []streamEventRecord) *streamShapeFixtureViolation {
	hasGolden := goldens != nil && goldens.hasGolden(cap)

	if kind == CapabilityBlind {
		if hasGolden {
			return &streamShapeFixtureViolation{
				Preset:      preset,
				Capability:  cap,
				Description: "blind capability has a golden fixture entry — remove it or write the rule",
			}
		}
		if fixtureExists {
			got, err := extractStreamShapeOutput(preset, cap, events)
			if err != nil {
				return &streamShapeFixtureViolation{
					Preset:      preset,
					Capability:  cap,
					Description: err.Error(),
				}
			}
			if streamShapeOutputPresent(cap, got) {
				return &streamShapeFixtureViolation{
					Preset:      preset,
					Capability:  cap,
					Description: "blind capability has extractable stream data — write the rule",
				}
			}
		}
		return nil
	}

	if kind == CapabilitySupported {
		if !fixtureExists {
			return &streamShapeFixtureViolation{
				Preset:      preset,
				Capability:  cap,
				Description: "supported capability has no captured stream fixture",
			}
		}
		got, err := extractStreamShapeOutput(preset, cap, events)
		if err != nil {
			return &streamShapeFixtureViolation{
				Preset:      preset,
				Capability:  cap,
				Description: err.Error(),
			}
		}
		present := streamShapeOutputPresent(cap, got)
		if !hasGolden {
			if present {
				return &streamShapeFixtureViolation{
					Preset:      preset,
					Capability:  cap,
					Description: "supported capability is missing a golden value for its stream fixture",
				}
			}
			return nil
		}
		want := streamShapeGoldenValue(goldens, cap)
		if !streamShapeOutputsMatch(cap, want, got) {
			return &streamShapeFixtureViolation{
				Preset:      preset,
				Capability:  cap,
				Description: streamShapeGoldenMismatch(cap, want, got),
			}
		}
		if !present {
			return &streamShapeFixtureViolation{
				Preset:      preset,
				Capability:  cap,
				Description: "supported capability golden does not prove the rule against its fixture",
			}
		}
	}

	return nil
}

// streamShapeFixtureGoldens are the expected outputs for trimmed real captured
// streams under testdata/streams/. Values were measured from the fixtures;
// update them when a fixture changes.
var streamShapeFixtureGoldens = map[string]*streamShapeGolden{
	"claude": {
		usage: &TokenUsage{
			Input: 1363, Output: 3690, CacheRead: 246817, CacheWrite: 34891,
			HasInput: true, HasOutput: true, HasCacheRead: true, HasCacheWrite: true,
		},
		toolTimings: []toolTimingGolden{
			{Name: "Bash", Count: 9, TotalNanos: (43*time.Second + 211*time.Millisecond).Nanoseconds()},
			{Name: "Read", Count: 1, TotalNanos: (51 * time.Millisecond).Nanoseconds()},
		},
		actualModel: strPtr("claude-opus-5"),
		streamRender: &streamRenderGolden{
			EventCount: 22,
			TypeCounts: map[string]int{"assistant": 1, "raw": 10, "system": 1, "tool_use": 10},
		},
		turn: &TurnCount{Count: 8, HasTurn: true},
	},
	"cursor": {
		turn: &TurnCount{Count: 67, HasTurn: true},
	},
	"codex": {
		toolTimings: []toolTimingGolden{
			{Name: "command_execution", Count: 1, TotalNanos: 0},
		},
	},
	"pi": {
		usage: &TokenUsage{
			Input: 66, Output: 3405, CacheRead: 71494, CacheWrite: 16868,
			HasInput: true, HasOutput: true, HasCacheRead: true, HasCacheWrite: true,
		},
		cost: &PartialCost{Dollars: 0.11416199999999999, HasCost: true},
		turn: &TurnCount{Count: 11, HasTurn: true},
	},
}

func strPtr(s string) *string { return &s }
