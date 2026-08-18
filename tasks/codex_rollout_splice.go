package tasks

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// codexRolloutAgent is the agent preset whose Captured runs carry a session
// rollout beside them.
const codexRolloutAgent = "codex"

// codexSessionsDirName is the subdirectory of codex's home holding the
// date-sharded session rollouts.
const codexSessionsDirName = "sessions"

// codexRolloutFilePrefix opens every rollout filename; the thread id closes it.
const codexRolloutFilePrefix = "rollout-"

// codexRolloutShardDepth bounds the YYYY/MM/DD walk under sessions/, so a
// stray deep tree there can never turn one persist into an unbounded crawl.
const codexRolloutShardDepth = 3

// codexTokenCountEvent is the rollout payload type the splice carries over: one
// per model call, the only rollout event pop stores (ADR-0219).
const codexTokenCountEvent = "token_count"

// spliceCodexRollout is the Rollout splice: it joins a codex run to codex's
// session rollout by the stream's thread.started thread id and returns the
// stored event sequence with the rollout's per-call token_count events merged
// in. codex's exec stream reports usage only as one whole-run rollup, so
// without this a stored codex run is turn-blind and peak-blind.
//
// Every failure to reach the rollout — another agent, no thread id, no sessions
// directory, no matching file, an unreadable file — returns the run's own
// events unchanged. A run stored unspliced reads blind, which is the honest
// answer; nothing here may fail a persist.
func spliceCodexRollout(d *Deps, agent string, events []streamEventRecord, start time.Time) []streamEventRecord {
	if agent != codexRolloutAgent || d == nil || d.FS == nil {
		return events
	}
	threadID := codexStreamThreadID(events)
	if threadID == "" {
		return events
	}
	path := findCodexRolloutFile(d.FS, codexSessionsDir(d.FS), threadID)
	if path == "" {
		return events
	}
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return events
	}
	spliced := codexRolloutTokenCountEvents(data, start)
	if len(spliced) == 0 {
		return events
	}
	return mergeStreamEventsByArrival(events, spliced)
}

// codexStreamThreadID reads the join key out of a captured codex stream: the
// thread id codex announces on thread.started, which is also the tail of its
// rollout filename.
func codexStreamThreadID(events []streamEventRecord) string {
	for _, ev := range events {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type == "thread.started" && event.ThreadID != "" {
			return event.ThreadID
		}
	}
	return ""
}

// codexSessionsDir resolves codex's rollout directory from codex's own home
// convention: CODEX_HOME when set, else ~/.codex. It returns "" when neither
// resolves, which reads downstream as "no rollout".
func codexSessionsDir(fs deps.FileSystem) string {
	home := strings.TrimSpace(fs.Getenv("CODEX_HOME"))
	if home == "" {
		userHome, err := fs.UserHomeDir()
		if err != nil || userHome == "" {
			return ""
		}
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, codexSessionsDirName)
}

// findCodexRolloutFile locates the rollout whose filename ends in the thread id,
// walking the date shards under sessions/ breadth-first to a fixed depth. Ties
// are broken by name so the same tree always yields the same file.
func findCodexRolloutFile(fs deps.FileSystem, sessionsDir, threadID string) string {
	if sessionsDir == "" || threadID == "" {
		return ""
	}
	suffix := "-" + threadID + ".jsonl"
	dirs := []string{sessionsDir}
	for depth := 0; depth <= codexRolloutShardDepth && len(dirs) > 0; depth++ {
		var next []string
		var matches []string
		for _, dir := range dirs {
			entries, err := fs.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() {
					next = append(next, filepath.Join(dir, name))
					continue
				}
				if strings.HasPrefix(name, codexRolloutFilePrefix) && strings.HasSuffix(name, suffix) {
					matches = append(matches, filepath.Join(dir, name))
				}
			}
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[0]
		}
		sort.Strings(next)
		dirs = next
	}
	return ""
}

// codexRolloutTokenCountEvents extracts the rollout's token_count events as
// stored stream records. Each record keeps the rollout payload verbatim, so a
// later reader sees codex's own vocabulary (info.last_token_usage,
// info.model_context_window) rather than a pop invention, and is timed by the
// rollout's own timestamp relative to the run's start.
func codexRolloutTokenCountEvents(data []byte, start time.Time) []streamEventRecord {
	var out []streamEventRecord
	var at int64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || len(entry.Payload) == 0 {
			continue
		}
		var payload struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(entry.Payload, &payload); err != nil || payload.Type != codexTokenCountEvent {
			continue
		}
		// An unparsable or pre-start timestamp inherits the previous event's
		// arrival, which keeps the spliced sequence in rollout order.
		if ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			if ms := ts.Sub(start).Milliseconds(); ms > at {
				at = ms
			}
		}
		out = append(out, streamEventRecord{Type: "event", AtMS: at, Raw: string(entry.Payload)})
	}
	return out
}

// mergeStreamEventsByArrival merges the spliced records into the run's own
// events by arrival time. The merge is stable and the run's own event wins a
// tie, so one run plus one rollout always produces the same stored sequence.
func mergeStreamEventsByArrival(own, spliced []streamEventRecord) []streamEventRecord {
	merged := make([]streamEventRecord, 0, len(own)+len(spliced))
	merged = append(merged, own...)
	merged = append(merged, spliced...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].AtMS < merged[j].AtMS })
	return merged
}
