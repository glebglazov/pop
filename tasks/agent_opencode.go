package tasks

import (
	"encoding/json"
	"strings"
)

func normalizeOpenCodeJSON(raw string) AgentResult {
	if pause := opencodeGoQuotaPauseReason(raw); pause != nil {
		return AgentResult{ProceedVerdict: pause}
	}
	var transcript strings.Builder
	var diagnostics []string
	var pause *AgentProceedVerdict
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
			Part  struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "text":
			if event.Part.Text == "" {
				break
			}
			// Each text event is a whole-message chunk (same framing the live
			// renderer applies). Join without a separator and a later message
			// that opens on SUMMARY_START / TASK_COMPLETE arrives glued to the
			// previous tail — the line-anchored completion readers miss it.
			if transcript.Len() > 0 && !strings.HasSuffix(transcript.String(), "\n") {
				transcript.WriteString("\n")
			}
			transcript.WriteString(event.Part.Text)
		case "error":
			detail := agentJSONDiagnostic(event.Error)
			if pause == nil {
				pause = opencodeGoQuotaPauseReason(detail)
			}
			if pause == nil {
				appendAgentDiagnostic(&diagnostics, detail)
			}
		}
		return true
	})
	if pause != nil {
		return AgentResult{ProceedVerdict: pause}
	}
	return normalizedTranscript(transcript.String(), diagnostics)
}

// openCodeLineRenderer renders opencode JSONL events live: assistant prose
// plain, and a dim "→ tool hint" tick per tool use. step_start, step_finish,
// and error render nothing; non-JSON lines are reported as unhandled so the
// writer passes them through raw. Each text event carries a whole-message
// chunk, so per-line newline framing works without fragment handling.
func openCodeLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text  string `json:"text"`
				Tool  string `json:"tool"`
				Title string `json:"title"`
				State struct {
					Input json.RawMessage `json:"input"`
				} `json:"state"`
			} `json:"part"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "text":
			if text := strings.TrimRight(event.Part.Text, "\n"); text != "" {
				return text + "\n", true
			}
			return "", true
		case "tool_use":
			return dim(openCodeToolTick(event.Part.Tool, event.Part.State.Input, event.Part.Title)) + "\n", true
		default:
			return "", true
		}
	}
}

// openCodeToolTick formats a compact "→ tool hint" line, probing the tool
// input for the first recognized salient key, then falling back to the
// pre-rendered part.title.
func openCodeToolTick(tool string, input json.RawMessage, title string) string {
	hint := openCodeToolHint(input)
	if hint == "" {
		hint = collapseHint(title)
	}
	return toolTick(tool, hint)
}

type openCodeToolHintProbe struct {
	FilePath string `json:"filePath"`
	Path     string `json:"path"`
	Command  string `json:"command"`
	Pattern  string `json:"pattern"`
	Query    string `json:"query"`
	URL      string `json:"url"`
}

func openCodeToolHint(input json.RawMessage) string {
	return toolHint(input, func(p openCodeToolHintProbe) string {
		return firstNonEmpty(p.FilePath, p.Path, p.Command, p.Pattern, p.Query, p.URL)
	})
}
