package release

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// memFS is an in-memory FileSystem backing the notice cache so writes are
// observable by later reads within a test.
type memFS struct {
	deps.MockFileSystem
	files map[string][]byte
}

func newMemFS() *memFS {
	m := &memFS{files: map[string][]byte{}}
	m.ReadFileFunc = func(path string) ([]byte, error) {
		data, ok := m.files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return data, nil
	}
	m.WriteFileFunc = func(path string, data []byte, _ os.FileMode) error {
		m.files[path] = append([]byte(nil), data...)
		return nil
	}
	m.MkdirAllFunc = func(string, os.FileMode) error { return nil }
	return m
}

const cachePath = "/data/pop/update.json"

// fixedFetcher returns the same tag (and optionally an error) and counts calls.
type fixedFetcher struct {
	tag   string
	err   error
	calls int
}

func (f *fixedFetcher) LatestReleaseTag() (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.tag, nil
}

// noticeDeps builds NoticeDeps with a synchronous background runner so the
// refresh is observable without goroutines.
func noticeDeps(fs *memFS, fetcher deps.ReleaseFetcher, now time.Time) *NoticeDeps {
	return &NoticeDeps{
		Fetcher:   fetcher,
		FS:        fs,
		Now:       func() time.Time { return now },
		CachePath: cachePath,
		Go:        func(f func()) { f() }, // synchronous
	}
}

func seedCache(t *testing.T, fs *memFS, c updateCache) {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal seed cache: %v", err)
	}
	fs.files[cachePath] = data
}

func readCache(t *testing.T, fs *memFS) updateCache {
	t.Helper()
	var c updateCache
	data, ok := fs.files[cachePath]
	if !ok {
		t.Fatalf("cache file %s not written", cachePath)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	return c
}

func TestPickerNotice_OutdatedShowsOncePerDay(t *testing.T) {
	day1 := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	// Fresh cache (checked today) advertising a newer release, never shown.
	seedCache(t, fs, updateCache{Latest: "v2026.6.1", CheckedAt: day1})

	latest, show := PickerNotice(noticeDeps(fs, &fixedFetcher{tag: "v2026.6.1"}, day1), "2026.6.0")
	if !show {
		t.Fatalf("first open of the day: want show=true")
	}
	if latest != "2026.6.1" {
		t.Errorf("latest = %q, want 2026.6.1", latest)
	}

	// shown_at must now be stamped to today.
	if got := readCache(t, fs); !sameDay(got.ShownAt, day1) {
		t.Errorf("shown_at = %v, want stamped to %v", got.ShownAt, day1)
	}

	// Second open the same day (a few hours later) is suppressed.
	day1Later := day1.Add(5 * time.Hour)
	_, show = PickerNotice(noticeDeps(fs, &fixedFetcher{tag: "v2026.6.1"}, day1Later), "2026.6.0")
	if show {
		t.Errorf("second open same day: want show=false")
	}

	// First open the next day shows again.
	day2 := day1.Add(24 * time.Hour)
	_, show = PickerNotice(noticeDeps(fs, &fixedFetcher{tag: "v2026.6.1"}, day2), "2026.6.0")
	if !show {
		t.Errorf("first open next day: want show=true")
	}
}

func TestPickerNotice_DevBuildNeverShows(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	seedCache(t, fs, updateCache{Latest: "v2026.6.1", CheckedAt: now})
	fetcher := &fixedFetcher{tag: "v2026.6.1"}

	latest, show := PickerNotice(noticeDeps(fs, fetcher, now), "2026.6.0-5-gabc123-dirty")
	if show {
		t.Errorf("dev build: want show=false")
	}
	if latest != "" {
		t.Errorf("dev build latest = %q, want empty", latest)
	}
	// Dev builds disable the automatic check entirely.
	if fetcher.calls != 0 {
		t.Errorf("dev build triggered %d fetches, want 0", fetcher.calls)
	}
}

func TestPickerNotice_CurrentDoesNotShow(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	seedCache(t, fs, updateCache{Latest: "v2026.6.0", CheckedAt: now})

	_, show := PickerNotice(noticeDeps(fs, &fixedFetcher{tag: "v2026.6.0"}, now), "2026.6.0")
	if show {
		t.Errorf("up-to-date binary: want show=false")
	}
}

func TestPickerNotice_FreshCacheDoesNotRefresh(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	seedCache(t, fs, updateCache{Latest: "v2026.6.0", CheckedAt: now.Add(-1 * time.Hour)})
	fetcher := &fixedFetcher{tag: "v2026.6.1"}

	PickerNotice(noticeDeps(fs, fetcher, now), "2026.6.0")
	if fetcher.calls != 0 {
		t.Errorf("fresh cache triggered %d fetches, want 0 (cache-only render)", fetcher.calls)
	}
}

func TestPickerNotice_StaleCacheRefreshesInBackground(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	// Stale cache (checked two days ago) that still thinks we're current.
	seedCache(t, fs, updateCache{Latest: "v2026.6.0", CheckedAt: now.Add(-48 * time.Hour)})
	fetcher := &fixedFetcher{tag: "v2026.6.1"}

	// This open renders from the stale cache (no notice) but schedules the
	// refresh, which the synchronous runner completes and persists.
	_, show := PickerNotice(noticeDeps(fs, fetcher, now), "2026.6.0")
	if show {
		t.Errorf("first open with stale (current) cache: want show=false")
	}
	if fetcher.calls != 1 {
		t.Fatalf("stale cache triggered %d fetches, want 1", fetcher.calls)
	}

	// The refresh result must be visible to a later open.
	got := readCache(t, fs)
	if got.Latest != "v2026.6.1" {
		t.Errorf("refreshed cache latest = %q, want v2026.6.1", got.Latest)
	}
	if !got.CheckedAt.Equal(now) {
		t.Errorf("refreshed checked_at = %v, want %v", got.CheckedAt, now)
	}

	// A later open the same day now sees the newer release and shows the notice.
	_, show = PickerNotice(noticeDeps(fs, &fixedFetcher{tag: "v2026.6.1"}, now.Add(time.Hour)), "2026.6.0")
	if !show {
		t.Errorf("later open after refresh: want show=true")
	}
}

func TestPickerNotice_MissingCacheNeverBlocksAndSchedulesRefresh(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS() // no cache file at all
	fetcher := &fixedFetcher{tag: "v2026.6.1"}

	// No cache → render nothing, but schedule the first refresh.
	latest, show := PickerNotice(noticeDeps(fs, fetcher, now), "2026.6.0")
	if show || latest != "" {
		t.Errorf("missing cache: want no notice, got show=%v latest=%q", show, latest)
	}
	if fetcher.calls != 1 {
		t.Errorf("missing cache triggered %d fetches, want 1", fetcher.calls)
	}
}

func TestPickerNotice_FailedRefreshIsSilent(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	fs := newMemFS()
	fetcher := &fixedFetcher{err: errors.New("network down")}

	// A failed background check must not panic, write a bogus cache, or show.
	latest, show := PickerNotice(noticeDeps(fs, fetcher, now), "2026.6.0")
	if show || latest != "" {
		t.Errorf("failed refresh: want no notice, got show=%v latest=%q", show, latest)
	}
	if _, ok := fs.files[cachePath]; ok {
		t.Errorf("failed refresh should not write a cache file")
	}
}
