package tasks

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestZZCheck(t *testing.T) {
	p := "/Users/glebglazov/.local/share/pop/repos/pop-b10dd6234b2d/tasks/2026-08-09-attended-agent-selection/streams/runs/2643dab0-f14f-456f-aa16-819495710023.events.jsonl.gz"
	f, _ := os.Open(p)
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	buf := new(strings.Builder)
	dec := json.NewDecoder(zr)
	for dec.More() {
		var rec struct {
			Raw string `json:"raw"`
		}
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		buf.WriteString(rec.Raw + "\n")
	}
	res := NormalizeAgentOutput(AgentOutputCursorStreamJSON, buf.String())
	a := AssessCompletion(res.Output, []byte("## Acceptance criteria\n\n- [x] x\n"))
	t.Logf("complete=%v reason=%q summary=%.80q", a.Complete, a.FailedReason, a.Summary)
	t.Logf("tail=%q", res.Output[max(0, len(res.Output)-200):])
}
