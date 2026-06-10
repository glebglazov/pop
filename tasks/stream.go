package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// streamsDirName is the Task-set subdirectory holding Captured attempt streams
// (ADR 0016). The name is the substrate, not one derived view: timing is the
// first lens over these files, not the last.
const streamsDirName = "streams"

// Footer outcomes. The kill outcomes distinguish why an attempt stopped,
// mirroring the Exhausted task / Interrupted task / Agent quota pause
// vocabulary, so a killed attempt's file still carries its terminal outcome.
const (
	streamOutcomeCompleted   = "completed"
	streamOutcomeFailed      = "failed"
	streamOutcomeTimedOut    = "timed_out"
	streamOutcomeInterrupted = "interrupted"
	streamOutcomeQuotaPaused = "quota_paused"
)

// streamHeaderRecord opens a Captured attempt stream file.
type streamHeaderRecord struct {
	Type      string    `json:"type"`
	Agent     string    `json:"agent"`
	Attempt   int       `json:"attempt"`
	StartTime time.Time `json:"start_time"`
}

// streamEventRecord is one raw stream event tagged with its arrival time
// relative to the attempt's start.
type streamEventRecord struct {
	Type string `json:"type"`
	AtMS int64  `json:"at_ms"`
	Raw  string `json:"raw"`
}

// streamFooterRecord closes a Captured attempt stream file.
type streamFooterRecord struct {
	Type       string `json:"type"`
	Outcome    string `json:"outcome"`
	DurationMS int64  `json:"duration_ms"`
}

// streamRecorder sits on the capture tee: it forwards raw bytes to the capture
// buffer unchanged and records each complete line with its arrival time. The
// timestamps live here — where the authoritative bytes are kept — so a
// rendering bug or an unrendered event type can never distort the persisted
// record (ADR 0016).
type streamRecorder struct {
	dst    io.Writer
	now    func() time.Time
	start  time.Time
	end    time.Time
	events []streamEventRecord
	buf    []byte
}

func newStreamRecorder(dst io.Writer, now func() time.Time) *streamRecorder {
	if now == nil {
		now = time.Now
	}
	return &streamRecorder{dst: dst, now: now, start: now()}
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	n, err := r.dst.Write(p)
	if err != nil {
		return n, err
	}
	at := r.now().Sub(r.start)
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		r.record(at, r.buf[:i])
		r.buf = r.buf[i+1:]
	}
	return len(p), nil
}

func (r *streamRecorder) record(at time.Duration, line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	r.events = append(r.events, streamEventRecord{
		Type: "event",
		AtMS: at.Milliseconds(),
		Raw:  string(line),
	})
}

// finish records any unterminated trailing line and stamps the attempt's end time.
func (r *streamRecorder) finish() {
	end := r.now()
	if len(r.buf) > 0 {
		r.record(end.Sub(r.start), r.buf)
		r.buf = nil
	}
	r.end = end
}

// encodeAttemptStream renders one self-contained JSONL document: header,
// timestamped raw events, footer.
func encodeAttemptStream(r *streamRecorder, agent string, attempt int, outcome string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: agent, Attempt: attempt, StartTime: r.start.UTC()}); err != nil {
		return nil, err
	}
	for _, ev := range r.events {
		if err := enc.Encode(ev); err != nil {
			return nil, err
		}
	}
	end := r.end
	if end.IsZero() {
		end = r.now()
	}
	if err := enc.Encode(streamFooterRecord{Type: "footer", Outcome: outcome, DurationMS: end.Sub(r.start).Milliseconds()}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// taskStreamDir returns the Captured-attempt-stream directory for one task:
// <task-set>/streams/<task-stem>.
func taskStreamDir(taskSetDir, taskFile string) string {
	stem := strings.TrimSuffix(taskFile, filepath.Ext(taskFile))
	return filepath.Join(taskSetDir, streamsDirName, stem)
}

var attemptStreamNamePattern = regexp.MustCompile(`^attempt-(\d+)\.jsonl\.gz$`)

// nextAttemptNumber returns one past the highest persisted attempt number, so
// numbering stays monotonic over the task's lifetime across implement
// invocations. A missing directory starts at 1.
func nextAttemptNumber(d *Deps, dir string) int {
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return 1
	}
	highest := 0
	for _, e := range entries {
		m := attemptStreamNamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > highest {
			highest = n
		}
	}
	return highest + 1
}

// writeAttemptStream gzips one encoded attempt stream into dir as
// attempt-NNN.jsonl.gz. Task storage is shared by all worktrees while the
// Runtime execution lock is per runtime path, so another implement can race
// for the same number: the file is opened O_CREATE|O_EXCL and NNN bumped on
// collision rather than widening the lock.
func writeAttemptStream(d *Deps, dir string, jsonl []byte) (string, error) {
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}

	for n := nextAttemptNumber(d, dir); ; n++ {
		path := filepath.Join(dir, fmt.Sprintf("attempt-%03d.jsonl.gz", n))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(gz.Bytes()); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
}

// persistAttemptStream writes one Captured attempt stream file best-effort: a
// storage failure is reported on errOut but never fails the implement run.
// A nil recorder (plain-output or custom-command attempt) records nothing.
// Returns the written file's path, or "" when nothing was persisted.
func persistAttemptStream(d *Deps, errOut io.Writer, sel *Selection, rec *streamRecorder, agent string, attempt int, outcome string) string {
	if rec == nil {
		return ""
	}
	var path string
	jsonl, err := encodeAttemptStream(rec, agent, attempt, outcome)
	if err == nil {
		path, err = writeAttemptStream(d, taskStreamDir(sel.Manifest.Dir, sel.TaskFile), jsonl)
	}
	if err != nil {
		fmt.Fprintf(errOut, "warning: persist attempt stream for %s/%s: %v\n", sel.TaskSetID, sel.TaskID, err)
		return ""
	}
	return path
}
