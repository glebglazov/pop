package drain

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// The Assist pane title names the attended entry the merged config resolves to,
// so the pane a human is about to work in says which agent is in it (ADR-0196
// decision 9, kept by ADR-0202 decision 5). An entry whose command names no
// model is named alone — nothing invents one.
func TestAssistPaneTitleNamesTheMergedAttendedEntry(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Cursor Usual", Cmd: "cursor"},
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	title := AssistPaneTitle("demo", attendedEntryLabel(cfg))
	if !strings.HasSuffix(title, " · Cursor Usual") {
		t.Fatalf("title = %q, want the merged head named alone", title)
	}
}
