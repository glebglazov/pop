package release

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
)

// updateCache is the persisted state behind the picker Update notice
// (CONTEXT.md "Update check", "Update notice"). The picker renders from this
// cache alone and never blocks on the network: Latest is the most recent
// Release tag a background refresh has seen, CheckedAt stamps that refresh, and
// ShownAt records the last day the notice was surfaced so it appears at most
// once per calendar day across all pickers.
type updateCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
	ShownAt   time.Time `json:"shown_at"`
}

// noticeRefreshInterval is the maximum age of the cache before a picker open
// schedules an async background refresh.
const noticeRefreshInterval = 24 * time.Hour

// NoticeDeps holds the dependencies behind the picker Update notice: the
// network seam (shared with the Doctor header), a filesystem for the cache, a
// clock, the cache path, and a runner for the background refresh. Tests inject
// mocks and a synchronous runner so the refresh is observable without
// goroutines or a real network.
type NoticeDeps struct {
	Fetcher   deps.ReleaseFetcher
	FS        deps.FileSystem
	Now       func() time.Time
	CachePath string
	// Go runs the background refresh closure. Production wraps it in `go`;
	// tests run it synchronously to observe the cache it writes.
	Go func(func())
}

// DefaultNoticeDeps returns notice dependencies wired to real implementations.
func DefaultNoticeDeps() *NoticeDeps {
	fs := deps.NewRealFileSystem()
	return &NoticeDeps{
		Fetcher:   deps.NewRealReleaseFetcher(),
		FS:        fs,
		Now:       time.Now,
		CachePath: defaultUpdateCachePath(fs),
		Go:        func(f func()) { go f() },
	}
}

// defaultUpdateCachePath returns the picker Update notice cache path in pop's
// data dir, respecting XDG_DATA_HOME with the ~/.local/share/pop fallback,
// consistent with the history and monitor-state paths.
func defaultUpdateCachePath(fs deps.FileSystem) string {
	if xdgData := fs.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "update.json")
	}
	home, err := fs.UserHomeDir()
	if err != nil {
		debug.Error("update notice: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop", "update.json")
}

// PickerNotice decides whether a picker should render the Update notice for the
// running version. It renders only from the persisted cache and never blocks on
// the network: a stale (or missing) cache schedules a background refresh whose
// result lands on a later open. The notice is suppressed for Dev builds and
// shown at most once per calendar day across all pickers — when it returns
// show=true it stamps ShownAt to today so the rest of the day's opens stay
// quiet. latest is the cached latest Release version (without the "v" prefix)
// when known.
func PickerNotice(d *NoticeDeps, current string) (latest string, show bool) {
	// Dev builds never show the notice and never trigger the automatic check.
	if !IsReleaseTag(current) {
		return "", false
	}

	cache := loadUpdateCache(d)
	now := d.Now()

	// Cache-only rendering: a cache older than the refresh interval (or one
	// never checked) schedules a background refresh for a later open. The
	// picker path itself never waits on it.
	if cache.CheckedAt.IsZero() || now.Sub(cache.CheckedAt) > noticeRefreshInterval {
		scheduleRefresh(d)
	}

	latest = normalizeVersion(cache.Latest)
	if !newerRelease(current, cache.Latest) {
		return latest, false
	}

	// Once per calendar day across all pickers: suppress if already shown today.
	if sameDay(cache.ShownAt, now) {
		return latest, false
	}

	cache.ShownAt = now
	saveUpdateCache(d, cache)
	return latest, true
}

// newerRelease reports whether latestTag is a Release strictly newer than the
// running current version. Either side failing to parse as a Release tag yields
// false (a Dev build, a malformed cache, or an unknown latest).
func newerRelease(current, latestTag string) bool {
	currentCV, okCur := parseCalVer(current)
	latestCV, okLatest := parseCalVer(latestTag)
	if !okCur || !okLatest {
		return false
	}
	return compareCalVer(latestCV, currentCV) > 0
}

// sameDay reports whether a and b fall on the same calendar day in b's
// location. A zero a (never shown) is never the same day.
func sameDay(a, b time.Time) bool {
	if a.IsZero() {
		return false
	}
	a = a.In(b.Location())
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// scheduleRefresh runs an async background Update check, writing the result to
// the cache for a later picker open. It re-reads the cache before writing so a
// ShownAt stamp set between scheduling and completion is preserved.
func scheduleRefresh(d *NoticeDeps) {
	run := d.Go
	if run == nil {
		run = func(f func()) { go f() }
	}
	run(func() {
		tag, err := d.Fetcher.LatestReleaseTag()
		if err != nil {
			debug.Error("update notice: background refresh: %v", err)
			return
		}
		cache := loadUpdateCache(d)
		cache.Latest = tag
		cache.CheckedAt = d.Now()
		saveUpdateCache(d, cache)
	})
}

// loadUpdateCache reads the cache file, returning a zero cache on any error
// (missing file, unreadable, or malformed) — the notice path is best-effort.
func loadUpdateCache(d *NoticeDeps) updateCache {
	var c updateCache
	data, err := d.FS.ReadFile(d.CachePath)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(data, &c); err != nil {
		debug.Error("update notice: unmarshal cache %s: %v", d.CachePath, err)
		return updateCache{}
	}
	return c
}

// saveUpdateCache writes the cache file, creating the data dir as needed.
// Errors are logged but never surfaced — the cache is best-effort.
func saveUpdateCache(d *NoticeDeps, c updateCache) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		debug.Error("update notice: marshal cache: %v", err)
		return
	}
	dir := filepath.Dir(d.CachePath)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		debug.Error("update notice: mkdir %s: %v", dir, err)
		return
	}
	if err := d.FS.WriteFile(d.CachePath, data, 0o644); err != nil {
		debug.Error("update notice: write %s: %v", d.CachePath, err)
	}
}
