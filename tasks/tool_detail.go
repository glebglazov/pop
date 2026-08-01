package tasks

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	suspectPeakInputThreshold = 200_000
	suspectTurnMedianFactor   = 2
	toolDetailTopPayloads     = 5
)

// ToolDetailReport is the argument-level tool breakdown for one attempt.
// Refused is true when the adapter's stream-render capability is blind.
type ToolDetailReport struct {
	Invocations   []toolInvocationRecord
	Refused       bool
	RefusalReason string
}

type toolInvocationRecord struct {
	Name            string
	ArgsKey         string
	ArgsHint        string
	ResultBytes     int
	IsError         bool
	IsUnboundedRead bool
	IsImageRead     bool
}

type toolOpenDetail struct {
	ID       string
	Name     string
	Args     json.RawMessage
	ArgsKey  string
	ArgsHint string
}

type toolCloseDetail struct {
	ID          string
	ResultBytes int
	IsError     bool
	Rename      func(pendingName string) string
}

// extractToolDetail derives argument-level tool facts from a Captured run.
// Render-blind adapters refuse with their declared stream-render reason
// (ADR-0165); supported adapters extract per-invocation records.
func extractToolDetail(agent string, events []streamEventRecord) ToolDetailReport {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return ToolDetailReport{
			Refused:       true,
			RefusalReason: toolDetailRefusal(agent, "unknown agent adapter"),
		}
	}
	cap := adapter.StreamRenderCapability()
	if cap.Kind != CapabilitySupported || cap.Render == nil {
		reason := cap.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "stream cannot be normalized"
		}
		return ToolDetailReport{
			Refused:       true,
			RefusalReason: toolDetailRefusal(agent, reason),
		}
	}
	switch agent {
	case "claude":
		return ToolDetailReport{Invocations: claudeToolDetail(events)}
	default:
		return ToolDetailReport{
			Refused:       true,
			RefusalReason: toolDetailRefusal(agent, "argument-level tool detail has not been implemented for this adapter"),
		}
	}
}

func toolDetailRefusal(agent, reason string) string {
	return fmt.Sprintf("%s: tool detail unavailable — %s", agent, reason)
}

// claudeToolDetail pairs each assistant tool_use with its user tool_result by
// id and records args, result size, and error status for each invocation.
func claudeToolDetail(events []streamEventRecord) []toolInvocationRecord {
	return accumulateToolInvocations(events, func(ev streamEventRecord) ([]toolOpenDetail, []toolCloseDetail) {
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
					IsError   bool            `json:"is_error"`
					Content   json.RawMessage `json:"content"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &msg); err != nil {
			return nil, nil
		}
		switch msg.Type {
		case "assistant":
			var opens []toolOpenDetail
			for _, c := range msg.Message.Content {
				if c.Type != "tool_use" || c.ID == "" {
					continue
				}
				opens = append(opens, toolOpenDetail{
					ID:       c.ID,
					Name:     c.Name,
					Args:     c.Input,
					ArgsKey:  canonicalArgsKey(c.Input),
					ArgsHint: claudeToolHint(c.Input),
				})
			}
			return opens, nil
		case "user":
			var closes []toolCloseDetail
			for _, c := range msg.Message.Content {
				if c.Type != "tool_result" || c.ToolUseID == "" {
					continue
				}
				closes = append(closes, toolCloseDetail{
					ID:          c.ToolUseID,
					ResultBytes: claudeToolResultBytes(c.Content),
					IsError:     c.IsError,
				})
			}
			return nil, closes
		default:
			return nil, nil
		}
	})
}

func claudeToolResultBytes(content json.RawMessage) int {
	if len(content) == 0 {
		return 0
	}
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return len(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err != nil {
		return len(content)
	}
	var n int
	for _, part := range parts {
		if part.Type == "text" {
			n += len(part.Text)
		}
	}
	return n
}

func accumulateToolInvocations(events []streamEventRecord, extract func(streamEventRecord) ([]toolOpenDetail, []toolCloseDetail)) []toolInvocationRecord {
	type pendingUse struct {
		name     string
		args     json.RawMessage
		argsKey  string
		argsHint string
	}
	pending := map[string]pendingUse{}
	var out []toolInvocationRecord
	for _, ev := range events {
		opens, closes := extract(ev)
		for _, open := range opens {
			pending[open.ID] = pendingUse{
				name:     open.Name,
				args:     open.Args,
				argsKey:  open.ArgsKey,
				argsHint: open.ArgsHint,
			}
		}
		for _, cl := range closes {
			use, ok := pending[cl.ID]
			if !ok {
				continue
			}
			delete(pending, cl.ID)
			name := use.name
			if cl.Rename != nil {
				name = cl.Rename(name)
			}
			out = append(out, toolInvocationRecord{
				Name:            name,
				ArgsKey:         use.argsKey,
				ArgsHint:        use.argsHint,
				ResultBytes:     cl.ResultBytes,
				IsError:         cl.IsError,
				IsUnboundedRead: isUnboundedRead(name, use.args),
				IsImageRead:     isImageRead(name, use.args),
			})
		}
	}
	return out
}

func canonicalArgsKey(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return string(args)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(args)
	}
	return string(b)
}

func readPathFromArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var probe struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return ""
	}
	return firstNonEmpty(probe.FilePath, probe.Path)
}

func isUnboundedRead(tool string, args json.RawMessage) bool {
	if tool != "Read" {
		return false
	}
	path := readPathFromArgs(args)
	if path == "" {
		return false
	}
	var probe struct {
		Limit *int `json:"limit"`
	}
	_ = json.Unmarshal(args, &probe)
	return probe.Limit == nil
}

var imageReadExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ico", ".tiff", ".tif",
}

func isImageRead(tool string, args json.RawMessage) bool {
	if tool != "Read" {
		return false
	}
	path := strings.ToLower(readPathFromArgs(args))
	if path == "" {
		return false
	}
	for _, ext := range imageReadExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

type toolDetailGroup struct {
	Label string
	Hint  string
	Count int
}

type toolDetailLine struct {
	Label string
	Extra string
}

func renderToolDetail(out *output, report ToolDetailReport) {
	if report.Refused {
		out.line(ansiDim, "    %s", report.RefusalReason)
		return
	}
	if len(report.Invocations) == 0 {
		return
	}

	repeated := groupToolDetailInvocations(report.Invocations, false)
	if len(repeated) > 0 {
		renderToolDetailSection(out, "repeated", repeated, func(g toolDetailGroup) toolDetailLine {
			return toolDetailLine{Label: formatToolDetailInvocation(g.Label, g.Hint)}
		})
	}

	unbounded := filterToolDetailInvocations(report.Invocations, func(inv toolInvocationRecord) bool {
		return inv.IsUnboundedRead
	})
	if len(unbounded) > 0 {
		renderToolDetailSection(out, "unbounded reads", groupToolDetailLines(unbounded), func(g toolDetailGroup) toolDetailLine {
			return toolDetailLine{Label: formatToolDetailInvocation(g.Label, g.Hint)}
		})
	}

	largest := largestToolDetailPayloads(report.Invocations, toolDetailTopPayloads)
	if len(largest) > 0 {
		out.line(ansiDim, "    largest payloads")
		for _, inv := range largest {
			out.line(ansiDim, "      %s  %s", formatToolDetailInvocation(inv.Name, inv.ArgsHint), humanizeBytes(inv.ResultBytes))
		}
	}

	errors := groupToolDetailInvocations(report.Invocations, true)
	if len(errors) > 0 {
		renderToolDetailSection(out, "errors", errors, func(g toolDetailGroup) toolDetailLine {
			return toolDetailLine{Label: formatToolDetailInvocation(g.Label, g.Hint)}
		})
	}

	images := filterToolDetailInvocations(report.Invocations, func(inv toolInvocationRecord) bool {
		return inv.IsImageRead
	})
	if len(images) > 0 {
		renderToolDetailSection(out, "image reads", groupToolDetailLines(images), func(g toolDetailGroup) toolDetailLine {
			return toolDetailLine{Label: formatToolDetailInvocation(g.Label, g.Hint)}
		})
	}
}

func formatToolDetailInvocation(name, hint string) string {
	if hint == "" {
		return name
	}
	return name + " " + hint
}

func renderToolDetailSection(out *output, title string, groups []toolDetailGroup, line func(toolDetailGroup) toolDetailLine) {
	if len(groups) == 0 {
		return
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	out.line(ansiDim, "    %s ×%d", title, total)
	for _, g := range groups {
		l := line(g)
		if l.Extra != "" {
			out.line(ansiDim, "      %s  %s", l.Label, l.Extra)
		} else {
			out.line(ansiDim, "      %s", l.Label)
		}
	}
}

func groupToolDetailInvocations(invocations []toolInvocationRecord, errorsOnly bool) []toolDetailGroup {
	counts := map[string]*toolDetailGroup{}
	for _, inv := range invocations {
		if errorsOnly && !inv.IsError {
			continue
		}
		key := inv.Name + "\x00" + inv.ArgsKey
		g := counts[key]
		if g == nil {
			g = &toolDetailGroup{Label: inv.Name, Hint: inv.ArgsHint}
			counts[key] = g
		}
		g.Count++
	}
	var out []toolDetailGroup
	for _, g := range counts {
		if g.Count > 1 {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		li := formatToolDetailInvocation(out[i].Label, out[i].Hint)
		lj := formatToolDetailInvocation(out[j].Label, out[j].Hint)
		return li < lj
	})
	return out
}

func filterToolDetailInvocations(invocations []toolInvocationRecord, keep func(toolInvocationRecord) bool) []toolInvocationRecord {
	var out []toolInvocationRecord
	for _, inv := range invocations {
		if keep(inv) {
			out = append(out, inv)
		}
	}
	return out
}

func groupToolDetailLines(invocations []toolInvocationRecord) []toolDetailGroup {
	counts := map[string]*toolDetailGroup{}
	for _, inv := range invocations {
		key := inv.Name + "\x00" + inv.ArgsKey
		g := counts[key]
		if g == nil {
			g = &toolDetailGroup{Label: inv.Name, Hint: inv.ArgsHint}
			counts[key] = g
		}
		g.Count++
	}
	out := make([]toolDetailGroup, 0, len(counts))
	for _, g := range counts {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		li := formatToolDetailInvocation(out[i].Label, out[i].Hint)
		lj := formatToolDetailInvocation(out[j].Label, out[j].Hint)
		return li < lj
	})
	return out
}

func largestToolDetailPayloads(invocations []toolInvocationRecord, limit int) []toolInvocationRecord {
	if limit <= 0 || len(invocations) == 0 {
		return nil
	}
	sorted := append([]toolInvocationRecord(nil), invocations...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ResultBytes != sorted[j].ResultBytes {
			return sorted[i].ResultBytes > sorted[j].ResultBytes
		}
		li := formatToolDetailInvocation(sorted[i].Name, sorted[i].ArgsHint)
		lj := formatToolDetailInvocation(sorted[j].Name, sorted[j].ArgsHint)
		return li < lj
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func medianTurnCount(attempts []AttemptStream) (int, bool) {
	var counts []int
	for _, a := range attempts {
		if a.Timing.Turns.HasTurn {
			counts = append(counts, a.Timing.Turns.Count)
		}
	}
	if len(counts) == 0 {
		return 0, false
	}
	sort.Ints(counts)
	n := len(counts)
	if n%2 == 1 {
		return counts[n/2], true
	}
	return (counts[n/2-1] + counts[n/2]) / 2, true
}

func suspectReasons(turns TurnCount, peak PeakInput, medianTurns int, hasMedian bool) []string {
	var reasons []string
	if peak.HasPeak && peak.Tokens > suspectPeakInputThreshold {
		reasons = append(reasons, fmt.Sprintf("peak-in %d > %d", peak.Tokens, suspectPeakInputThreshold))
	}
	if hasMedian && turns.HasTurn && turns.Count > suspectTurnMedianFactor*medianTurns {
		reasons = append(reasons, fmt.Sprintf("turns %d > %d×median %d", turns.Count, suspectTurnMedianFactor, medianTurns))
	}
	return reasons
}

func renderAttemptSuspects(out *output, timing AttemptTiming, medianTurns int, hasMedian bool) {
	reasons := suspectReasons(timing.Turns, timing.PeakInput, medianTurns, hasMedian)
	if len(reasons) == 0 {
		return
	}
	out.line(ansiYellow, "    suspect: %s", strings.Join(reasons, "; "))
}
