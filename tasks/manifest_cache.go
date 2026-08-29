package tasks

import "encoding/json"

// The persisted tier of the Manifest memo (ADR-0243 decisions 1, 2 and 4).
//
// The in-process tier answers a poll; this one answers the first paint. A fresh
// `pop work dashboard` otherwise re-reads every task markdown on the machine to
// re-derive answers the last process already derived — which is the whole of the
// gap between a ~380ms cold paint and an ~85ms warm one, because a content-key
// hit never opens a markdown at all.
//
// It changes what a miss costs and never what a hit means. The key is the same
// key: the manifest's bytes plus every dirent's size, mtime and name, recomputed
// from the directory on every serve and compared before anything is handed back.
// That comparison is what separates this from the glob cache ADR-0242 deleted
// for going stale invisibly — here a stale serve is not unlikely, it cannot be
// expressed.
//
// Every failure is a miss. Nothing in this file returns an error, because there
// is no caller that could do anything with one but re-validate, which is what a
// miss already makes it do.

// persistedManifestEntry returns dir's persisted manifest when the row was
// written under contentKey. The manifest it returns is decoded fresh from bytes
// and shared with nobody, so the caller may mutate it exactly as it mutates a
// freshly loaded one — serialisation gives for free what clone() gives the
// in-process tier.
func persistedManifestEntry(d *Deps, dir, contentKey string) (*Manifest, bool) {
	payload, ok := d.CacheDB().ManifestEntry(dir, contentKey)
	if !ok {
		return nil, false
	}
	var record persistedManifest
	if err := json.Unmarshal(payload, &record); err != nil {
		// A row this build cannot read is a row that is not there. The next miss
		// overwrites it.
		return nil, false
	}
	return record.manifest(), true
}

// persistManifestEntry makes the persisted tier carry m as dir's manifest under
// contentKey, at most once per key per cache handle.
//
// It is called on a process-tier hit as well as on a miss, because a hit says
// nothing about the row on disk. The **Work supervisor** is why that matters: it
// ticks for days holding one process tier, so a set validated on its first tick
// would otherwise be offered to the cache exactly once — and a human who deletes
// cache.db to repair it (a supported repair) would get a fresh database that
// every dashboard opens cold until the daemon restarts.
//
// The once-per-handle bookkeeping is what keeps that cheap: without it a warm
// poll would upsert every set it walks, every two seconds, forever. A write that
// does not land is not remembered, so the next tick offers it again.
func persistManifestEntry(d *Deps, dir, contentKey string, m *Manifest) {
	warmed := d.cacheDBWarmed()
	if warmed == nil {
		// Nothing to persist to, and the encode is worth skipping: a machine with
		// no usable cache pays this on every set of every walk.
		return
	}
	if _, written := warmed.Get(contentKey); written {
		return
	}
	payload, err := json.Marshal(newPersistedManifest(m))
	if err != nil {
		return
	}
	if d.CacheDB().PutManifestEntry(dir, contentKey, payload) {
		warmed.Put(contentKey, struct{}{})
	}
}

// markManifestPersisted records that the persisted tier already carries
// contentKey, so serving from it does not schedule a write of what was just read.
func markManifestPersisted(d *Deps, contentKey string) {
	if warmed := d.cacheDBWarmed(); warmed != nil {
		warmed.Put(contentKey, struct{}{})
	}
}

// persistedManifest is the stored shape of a manifest: a total mirror of
// Manifest, field for field, rather than Manifest itself.
//
// Manifest cannot be marshalled directly because Task's own JSON shape is
// authored-manifest shape, not memory shape — it exists to keep a rewritten
// index.json tidy, so it drops the effort key it was loaded without and carries
// no EffortExplicit at all. A tier that served that back would hand out a
// manifest subtly unlike the one a fresh load returns. This mirror carries every
// field, and manifestCacheMirrorsEveryField pins that a field added to Manifest
// or Task is added here too.
//
// Raw and Unknown ride as []byte rather than json.RawMessage for the same
// faithfulness: a nil RawMessage marshals to the literal `null` and comes back
// as the four bytes "null", where a freshly loaded manifest would have nil.
type persistedManifest struct {
	Stem               string            `json:"stem"`
	Dir                string            `json:"dir"`
	Path               string            `json:"path"`
	Tasks              []persistedTask   `json:"tasks"`
	Raw                []byte            `json:"raw"`
	Errors             []string          `json:"errors"`
	Valid              bool              `json:"valid"`
	Unknown            map[string][]byte `json:"unknown"`
	SourceMap          string            `json:"source_map"`
	BaseCommit         string            `json:"base_commit"`
	BaseCommitRecorded bool              `json:"base_commit_recorded"`
	CommitConvention   string            `json:"commit_convention"`
	HumanCompleted     bool              `json:"human_completed"`
	DeprecatedKeys     []string          `json:"deprecated_keys"`
}

type persistedTask struct {
	ID             string      `json:"id"`
	File           string      `json:"file"`
	Title          string      `json:"title"`
	Type           string      `json:"type"`
	Status         TaskStatus  `json:"status"`
	BlockedBy      []string    `json:"blocked_by"`
	FailedAfter    *int        `json:"failed_after"`
	Effort         string      `json:"effort"`
	EffortExplicit bool        `json:"effort_explicit"`
	Origin         string      `json:"origin"`
	Commit         *TaskCommit `json:"commit"`
	CommitSubject  string      `json:"commit_subject"`
}

func newPersistedManifest(m *Manifest) persistedManifest {
	record := persistedManifest{
		Stem:               m.Stem,
		Dir:                m.Dir,
		Path:               m.Path,
		Raw:                m.Raw,
		Errors:             m.Errors,
		Valid:              m.Valid,
		SourceMap:          m.SourceMap,
		BaseCommit:         m.BaseCommit,
		BaseCommitRecorded: m.BaseCommitRecorded,
		CommitConvention:   m.CommitConvention,
		HumanCompleted:     m.HumanCompleted,
		DeprecatedKeys:     m.DeprecatedKeys,
	}
	if m.Tasks != nil {
		record.Tasks = make([]persistedTask, len(m.Tasks))
		for i, task := range m.Tasks {
			record.Tasks[i] = persistedTask{
				ID:             task.ID,
				File:           task.File,
				Title:          task.Title,
				Type:           task.Type,
				Status:         task.Status,
				BlockedBy:      task.BlockedBy,
				FailedAfter:    task.FailedAfter,
				Effort:         task.Effort,
				EffortExplicit: task.EffortExplicit,
				Origin:         task.Origin,
				Commit:         task.Commit,
				CommitSubject:  task.CommitSubject,
			}
		}
	}
	if m.Unknown != nil {
		record.Unknown = make(map[string][]byte, len(m.Unknown))
		for k, v := range m.Unknown {
			record.Unknown[k] = v
		}
	}
	return record
}

// manifest rebuilds the loaded shape. Every slice and map it hands over is its
// own — decoding allocated them — so the caller owns what it gets.
func (r persistedManifest) manifest() *Manifest {
	m := &Manifest{
		Stem:               r.Stem,
		Dir:                r.Dir,
		Path:               r.Path,
		Raw:                json.RawMessage(r.Raw),
		Errors:             r.Errors,
		Valid:              r.Valid,
		SourceMap:          r.SourceMap,
		BaseCommit:         r.BaseCommit,
		BaseCommitRecorded: r.BaseCommitRecorded,
		CommitConvention:   r.CommitConvention,
		HumanCompleted:     r.HumanCompleted,
		DeprecatedKeys:     r.DeprecatedKeys,
	}
	if r.Tasks != nil {
		m.Tasks = make([]Task, len(r.Tasks))
		for i, task := range r.Tasks {
			m.Tasks[i] = Task{
				ID:             task.ID,
				File:           task.File,
				Title:          task.Title,
				Type:           task.Type,
				Status:         task.Status,
				BlockedBy:      task.BlockedBy,
				FailedAfter:    task.FailedAfter,
				Effort:         task.Effort,
				EffortExplicit: task.EffortExplicit,
				Origin:         task.Origin,
				Commit:         task.Commit,
				CommitSubject:  task.CommitSubject,
			}
		}
	}
	if r.Unknown != nil {
		m.Unknown = make(map[string]json.RawMessage, len(r.Unknown))
		for k, v := range r.Unknown {
			m.Unknown[k] = json.RawMessage(v)
		}
	}
	return m
}
