// Package release implements pop's Update check: querying the latest Release,
// CalVer comparison, and exactly-a-release-tag (Dev build) detection. It is the
// reusable core shared by Doctor's version header and the picker's Update
// notice. The network call lives behind a deps.ReleaseFetcher seam; everything
// else here is pure and unit-tested without a network.
package release

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the release package.
type Deps struct {
	Fetcher deps.ReleaseFetcher
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		Fetcher: deps.NewRealReleaseFetcher(),
	}
}

// State is the outcome of an Update check.
type State int

const (
	// StateCurrent: the running binary is a Release and is the latest one.
	StateCurrent State = iota
	// StateOutdated: a newer Release exists than the running binary.
	StateOutdated
	// StateDev: the running binary is a Dev build; no comparison is made.
	StateDev
	// StateFailed: the check could not be completed (network/API error).
	StateFailed
)

// Result is the full outcome of an Update check, carrying both the running
// version and (when known) the latest Release for rendering.
type Result struct {
	// Current is the running binary's version, without the "v" prefix.
	Current string
	// Latest is the latest Release version without the "v" prefix. Empty for
	// Dev builds and failed checks.
	Latest string
	// State classifies the outcome.
	State State
	// Err is the underlying error when State is StateFailed.
	Err error
}

// releaseTagPattern matches an exact CalVer release tag (vYYYY.M.N), with an
// optional leading "v". A Dev build's version never matches: it is a
// tag-relative describe string (v2026.6.0-5-gabc123-dirty) or a bare SHA.
var releaseTagPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// IsReleaseTag reports whether version is exactly a Release tag, i.e. the
// running binary is a Release rather than a Dev build.
func IsReleaseTag(version string) bool {
	return releaseTagPattern.MatchString(strings.TrimSpace(version))
}

// calver is a parsed CalVer version: Year.Month.Counter.
type calver struct {
	year    int
	month   int
	counter int
}

// parseCalVer parses a release tag (with or without the "v" prefix) into its
// three numeric components. It returns false if version is not an exact
// release tag.
func parseCalVer(version string) (calver, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !releaseTagPattern.MatchString(v) {
		return calver{}, false
	}
	parts := strings.Split(v, ".")
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	counter, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return calver{}, false
	}
	return calver{year: year, month: month, counter: counter}, true
}

// compareCalVer returns -1 if a < b, 0 if equal, +1 if a > b.
func compareCalVer(a, b calver) int {
	switch {
	case a.year != b.year:
		return cmpInt(a.year, b.year)
	case a.month != b.month:
		return cmpInt(a.month, b.month)
	default:
		return cmpInt(a.counter, b.counter)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// normalizeVersion strips the "v" prefix for display.
func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// Check performs a live Update check for the given running version using real
// dependencies.
func Check(current string) Result {
	return CheckWith(DefaultDeps(), current)
}

// CheckWith performs a live Update check against injected dependencies.
//
// Rules (CONTEXT.md "Releases"):
//   - A Dev build (current is not exactly a release tag) yields StateDev with
//     no comparison and no network result requirement.
//   - A failed fetch yields StateFailed; it is never a hard error.
//   - Otherwise CalVer-compare current against the latest Release.
func CheckWith(d *Deps, current string) Result {
	res := Result{Current: normalizeVersion(current)}

	if !IsReleaseTag(current) {
		res.State = StateDev
		return res
	}

	latestTag, err := d.Fetcher.LatestReleaseTag()
	if err != nil {
		res.State = StateFailed
		res.Err = err
		return res
	}

	currentCV, _ := parseCalVer(current)
	latestCV, ok := parseCalVer(latestTag)
	if !ok {
		res.State = StateFailed
		return res
	}

	res.Latest = normalizeVersion(latestTag)
	if compareCalVer(latestCV, currentCV) > 0 {
		res.State = StateOutdated
	} else {
		res.State = StateCurrent
	}
	return res
}
