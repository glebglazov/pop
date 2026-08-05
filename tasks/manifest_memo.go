package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/glebglazov/pop/internal/deps"
)

// manifestMemoCapacity bounds how many set directories the process remembers a
// validated manifest for. It is a working-set bound, not a machine inventory:
// the surfaces that share this memo — the three passes `pop work status` makes
// over a repo group, both dashboard pages, the Map kind's duplicate refresh,
// every 2s poll after the first — all walk the same handful of definition paths,
// so a few hundred sets covers every set a machine has registered several times
// over. Past it the least-recently-walked set is evicted and simply re-validates
// the next time something asks, which is what keeps the daemon's memo from
// growing for as long as the daemon lives.
const manifestMemoCapacity = 512

// manifestMemo caches LoadManifest's whole answer for the life of the process,
// keyed on the content of the set directory it was derived from (ADR-0189). It is
// process-wide rather than per-load because the cost it removes is the repeat: a
// poll that re-walks an unchanged definition path pays a full read plus a
// line-split plus two regexes per line for every task markdown in every set, and
// nothing about that answer depends on when it was asked.
var manifestMemo = deps.NewContentMemo[*Manifest](manifestMemoCapacity)

// manifestContentKey names every file input LoadManifest has, so an entry can
// never serve an answer the directory no longer supports. Three parts, each
// load-bearing:
//
//   - the manifest's bytes, which decide the task list and every per-entry rule;
//   - each directory entry's size and mtime, which is how an edited task markdown
//     that gains or loses its acceptance-criteria section invalidates;
//   - the set of names, because a markdown the manifest never mentions is what
//     flips a set to MALFORMED through the orphan check. Keyed on the manifest
//     alone the memo would go stale on a file the manifest does not name.
//
// The listing is the whole directory rather than the listed task files: it is one
// syscall either way, and covering names the manifest does not know about is the
// point. Not covered is a task file the manifest names outside the directory
// (`"file": "sub/x.md"`), which is already an error in its own right — the set is
// MALFORMED whatever that file says, so nothing about it can flip validity.
//
// Returns false when the key cannot be trusted, which is a plain cache miss: the
// caller then validates as if no memo existed.
func manifestContentKey(manifestPath string, manifestData []byte, entries []os.DirEntry) (string, bool) {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00", manifestPath, len(manifestData))
	h.Write(manifestData)

	// Sorted because a fake filesystem may list in any order, while the answer
	// derived from the listing does not depend on it.
	stamps := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", false
		}
		kind := "f"
		if entry.IsDir() {
			kind = "d"
		}
		stamps = append(stamps, fmt.Sprintf("%s\x00%s\x00%d\x00%d", entry.Name(), kind, info.Size(), info.ModTime().UnixNano()))
	}
	sort.Strings(stamps)
	for _, stamp := range stamps {
		fmt.Fprintf(h, "%s\x00", stamp)
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// clone is what makes a memoized manifest safe to hand out: callers mutate what
// LoadManifest returns — a task's status on a transition, HumanCompleted on the
// way out of terminal — and a shared entry would carry that edit to the next
// reader as though the file said so. Nil-ness of each slice is preserved, so a
// served manifest compares equal to a freshly loaded one.
func (m *Manifest) clone() *Manifest {
	if m == nil {
		return nil
	}
	copied := *m
	copied.Tasks = make([]Task, len(m.Tasks))
	for i, task := range m.Tasks {
		task.BlockedBy = append([]string(nil), task.BlockedBy...)
		if task.FailedAfter != nil {
			failedAfter := *task.FailedAfter
			task.FailedAfter = &failedAfter
		}
		copied.Tasks[i] = task
	}
	if m.Tasks == nil {
		copied.Tasks = nil
	}
	copied.Raw = append(json.RawMessage(nil), m.Raw...)
	copied.Errors = append([]string(nil), m.Errors...)
	copied.DeprecatedKeys = append([]string(nil), m.DeprecatedKeys...)
	if m.Unknown != nil {
		copied.Unknown = make(map[string]json.RawMessage, len(m.Unknown))
		for k, v := range m.Unknown {
			copied.Unknown[k] = append(json.RawMessage(nil), v...)
		}
	}
	return &copied
}
