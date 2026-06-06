package release

import (
	"errors"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestIsReleaseTag(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"2026.6.0", true},
		{"v2026.6.0", true},
		{"2026.12.15", true},
		{"v2026.6.0-5-gabc123-dirty", false}, // tag-relative describe (dev build)
		{"2026.6.0-5-gabc123", false},
		{"abc1234", false},       // bare SHA
		{"abc1234-dirty", false}, // dirty SHA
		{"dev", false},           // go run
		{"unknown", false},       // no VCS info
		{"", false},              // empty
		{"2026.6", false},        // missing counter
		{"2026.6.0.1", false},    // too many components
		{"  2026.6.0  ", true},   // surrounding whitespace tolerated
	}
	for _, c := range cases {
		if got := IsReleaseTag(c.version); got != c.want {
			t.Errorf("IsReleaseTag(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}

func TestParseAndCompareCalVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.6.0", "2026.6.1", -1}, // counter
		{"2026.6.1", "2026.6.0", 1},
		{"2026.6.0", "2026.6.0", 0},
		{"2026.6.0", "2026.7.0", -1},  // month
		{"2026.12.0", "2027.1.0", -1}, // year rolls over
		{"v2026.6.0", "2026.6.0", 0},  // v-prefix normalized
		{"2026.6.9", "2026.6.10", -1}, // numeric, not lexical
	}
	for _, c := range cases {
		acv, ok := parseCalVer(c.a)
		if !ok {
			t.Fatalf("parseCalVer(%q) failed", c.a)
		}
		bcv, ok := parseCalVer(c.b)
		if !ok {
			t.Fatalf("parseCalVer(%q) failed", c.b)
		}
		if got := compareCalVer(acv, bcv); got != c.want {
			t.Errorf("compareCalVer(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func fetcher(tag string, err error) *Deps {
	return &Deps{Fetcher: &deps.MockReleaseFetcher{
		LatestReleaseTagFunc: func() (string, error) { return tag, err },
	}}
}

func TestCheckWith_Outdated(t *testing.T) {
	res := CheckWith(fetcher("v2026.6.1", nil), "v2026.6.0")
	if res.State != StateOutdated {
		t.Fatalf("state = %v, want StateOutdated", res.State)
	}
	if res.Current != "2026.6.0" || res.Latest != "2026.6.1" {
		t.Errorf("got current=%q latest=%q", res.Current, res.Latest)
	}
}

func TestCheckWith_Current(t *testing.T) {
	res := CheckWith(fetcher("v2026.6.0", nil), "v2026.6.0")
	if res.State != StateCurrent {
		t.Fatalf("state = %v, want StateCurrent", res.State)
	}
	if res.Latest != "2026.6.0" {
		t.Errorf("latest = %q", res.Latest)
	}
}

func TestCheckWith_NewerLocalStillCurrent(t *testing.T) {
	// A locally built tag ahead of the published latest is not "outdated".
	res := CheckWith(fetcher("v2026.6.0", nil), "v2026.6.5")
	if res.State != StateCurrent {
		t.Fatalf("state = %v, want StateCurrent", res.State)
	}
}

func TestCheckWith_DevBuildSkipsNetwork(t *testing.T) {
	called := false
	d := &Deps{Fetcher: &deps.MockReleaseFetcher{
		LatestReleaseTagFunc: func() (string, error) { called = true; return "v2026.6.1", nil },
	}}
	res := CheckWith(d, "v2026.6.0-5-gabc123-dirty")
	if res.State != StateDev {
		t.Fatalf("state = %v, want StateDev", res.State)
	}
	if called {
		t.Error("dev build must not query the network")
	}
	if res.Current != "2026.6.0-5-gabc123-dirty" {
		t.Errorf("current = %q", res.Current)
	}
}

func TestCheckWith_FetchFailure(t *testing.T) {
	res := CheckWith(fetcher("", errors.New("network down")), "v2026.6.0")
	if res.State != StateFailed {
		t.Fatalf("state = %v, want StateFailed", res.State)
	}
	if res.Err == nil {
		t.Error("expected Err to be set on failure")
	}
}

func TestCheckWith_GarbageLatestTagFails(t *testing.T) {
	res := CheckWith(fetcher("not-a-tag", nil), "v2026.6.0")
	if res.State != StateFailed {
		t.Fatalf("state = %v, want StateFailed", res.State)
	}
}
