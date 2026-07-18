package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// --- fixtures ------------------------------------------------------------

// mwSub is a nested sub-table descended into by the fields kind.
type mwSub struct {
	A string `toml:"a" merge:"replace" include:"replace"`
	B string `toml:"b" merge:"replace" include:"replace"`
}

// mwBench is a list-by-key element keyed by its name.
type mwBench struct {
	Name string `toml:"name"`
	Val  string `toml:"val"`
}

// mwFixture exercises every granularity kind. Untagged carries no include: tag,
// so it is the not-whitelisted probe.
type mwFixture struct {
	Scalar   string            `toml:"scalar" merge:"replace" include:"replace"`
	Untagged string            `toml:"untagged"`
	Nested   *mwSub            `toml:"nested" merge:"fields" include:"fields"`
	M        map[string]string `toml:"m" merge:"map" include:"map"`
	MFW      map[string]string `toml:"mfw" merge:"map-first-wins" include:"map-first-wins"`
	List     []string          `toml:"list" merge:"append" include:"append"`
	Benches  []mwBench         `toml:"benches" merge:"list-by-key=name" include:"list-by-key=name"`
}

func decodeFixture(t *testing.T, body string) (mwFixture, toml.MetaData) {
	t.Helper()
	var f mwFixture
	md, err := toml.Decode(body, &f)
	if err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return f, md
}

// runMerge merges src (decoded from srcBody) into dst (decoded from dstBody)
// under an overlay or include policy and returns the result plus the paths the
// policy's callbacks saw.
func runMerge(t *testing.T, dstBody, srcBody string, include bool) (mwFixture, []string, []string) {
	t.Helper()
	dst, _ := decodeFixture(t, dstBody)
	src, srcMD := decodeFixture(t, srcBody)
	var collisions, notWhitelisted []string
	var policy *mergePolicy
	onCollision := func(p string) { collisions = append(collisions, p) }
	if include {
		policy = includePolicy(
			onCollision,
			func(p string) { notWhitelisted = append(notWhitelisted, p) },
		)
	} else {
		// Overlay is silent in production (nil callback); wire an observer here so
		// the kinds that report collisions regardless of mode can be asserted.
		policy = overlayPolicy()
		policy.onCollision = onCollision
	}
	mergeWalk(&dst, &src, srcMD, policy)
	return dst, collisions, notWhitelisted
}

// --- per-kind table test -------------------------------------------------

func TestMergeWalkKinds(t *testing.T) {
	tests := []struct {
		name       string
		dstBody    string
		srcBody    string
		include    bool
		collisions []string
		check      func(t *testing.T, got mwFixture)
	}{
		{
			name:    "replace last-wins",
			dstBody: `scalar = "old"`,
			srcBody: `scalar = "new"`,
			check: func(t *testing.T, got mwFixture) {
				if got.Scalar != "new" {
					t.Errorf("Scalar = %q, want new", got.Scalar)
				}
			},
		},
		{
			name:    "replace absent leaves dst intact",
			dstBody: `scalar = "keep"`,
			srcBody: `list = ["x"]`,
			check: func(t *testing.T, got mwFixture) {
				if got.Scalar != "keep" {
					t.Errorf("Scalar = %q, want keep (src did not define it)", got.Scalar)
				}
			},
		},
		{
			name:    "fields descends, only defined sub-key overrides",
			dstBody: "[nested]\na = \"1\"\nb = \"2\"",
			srcBody: "[nested]\na = \"9\"",
			check: func(t *testing.T, got mwFixture) {
				if got.Nested == nil || got.Nested.A != "9" || got.Nested.B != "2" {
					t.Errorf("Nested = %+v, want {A:9 B:2}", got.Nested)
				}
			},
		},
		{
			name:    "map per-key last-wins overwrites and adds",
			dstBody: "[m]\nk1 = \"v1\"",
			srcBody: "[m]\nk1 = \"v1b\"\nk2 = \"v2\"",
			check: func(t *testing.T, got mwFixture) {
				want := map[string]string{"k1": "v1b", "k2": "v2"}
				if !reflect.DeepEqual(got.M, want) {
					t.Errorf("M = %v, want %v", got.M, want)
				}
			},
		},
		{
			name:       "map-first-wins keeps existing key and reports collision",
			dstBody:    "[mfw]\nk1 = \"v1\"",
			srcBody:    "[mfw]\nk1 = \"v1b\"\nk2 = \"v2\"",
			collisions: []string{"mfw.k1"},
			check: func(t *testing.T, got mwFixture) {
				want := map[string]string{"k1": "v1", "k2": "v2"}
				if !reflect.DeepEqual(got.MFW, want) {
					t.Errorf("MFW = %v, want %v", got.MFW, want)
				}
			},
		},
		{
			name:    "append concatenates onto dst",
			dstBody: `list = ["a"]`,
			srcBody: `list = ["b", "c"]`,
			check: func(t *testing.T, got mwFixture) {
				want := []string{"a", "b", "c"}
				if !reflect.DeepEqual(got.List, want) {
					t.Errorf("List = %v, want %v", got.List, want)
				}
			},
		},
		{
			name:       "list-by-key last-wins replaces collision, appends new",
			dstBody:    "[[benches]]\nname = \"x\"\nval = \"1\"",
			srcBody:    "[[benches]]\nname = \"x\"\nval = \"2\"\n[[benches]]\nname = \"y\"\nval = \"3\"",
			collisions: []string{"benches[x]"},
			check: func(t *testing.T, got mwFixture) {
				want := []mwBench{{Name: "x", Val: "2"}, {Name: "y", Val: "3"}}
				if !reflect.DeepEqual(got.Benches, want) {
					t.Errorf("Benches = %+v, want %+v", got.Benches, want)
				}
			},
		},
		{
			name:       "list-by-key first-wins keeps collision, appends new",
			dstBody:    "[[benches]]\nname = \"x\"\nval = \"1\"",
			srcBody:    "[[benches]]\nname = \"x\"\nval = \"2\"\n[[benches]]\nname = \"y\"\nval = \"3\"",
			include:    true,
			collisions: []string{"benches[x]"},
			check: func(t *testing.T, got mwFixture) {
				want := []mwBench{{Name: "x", Val: "1"}, {Name: "y", Val: "3"}}
				if !reflect.DeepEqual(got.Benches, want) {
					t.Errorf("Benches = %+v, want %+v", got.Benches, want)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, collisions, _ := runMerge(t, tc.dstBody, tc.srcBody, tc.include)
			if !stringsEqual(collisions, tc.collisions) {
				t.Errorf("collisions = %v, want %v", collisions, tc.collisions)
			}
			tc.check(t, got)
		})
	}
}

// --- presence is toml.MetaData-driven -----------------------------------

func TestMergeWalkPresenceIsMetadataDriven(t *testing.T) {
	// A defined-but-zero value still overrides (metadata says it was written),
	// while a table with only one sub-key defined leaves the sibling untouched.
	dst, _ := decodeFixture(t, `scalar = "keep"`+"\n[nested]\na = \"1\"\nb = \"2\"")
	src, srcMD := decodeFixture(t, `scalar = ""`+"\n[nested]\nb = \"9\"")

	// Sanity: metadata records only the keys the source actually wrote.
	if !srcMD.IsDefined("nested", "b") || srcMD.IsDefined("nested", "a") {
		t.Fatalf("metadata should mark nested.b defined and nested.a absent")
	}

	mergeWalk(&dst, &src, srcMD, overlayPolicy())

	if dst.Scalar != "" {
		t.Errorf("Scalar = %q, want empty (defined-but-zero overrides)", dst.Scalar)
	}
	if dst.Nested.A != "1" {
		t.Errorf("Nested.A = %q, want 1 (undefined sub-key preserved)", dst.Nested.A)
	}
	if dst.Nested.B != "9" {
		t.Errorf("Nested.B = %q, want 9 (defined sub-key overrides)", dst.Nested.B)
	}
}

// --- first-wins claim tracking across a sequence of sources -------------

func TestMergeWalkFirstWinsAcrossSources(t *testing.T) {
	var dst mwFixture
	var collisions []string
	policy := includePolicy(func(p string) { collisions = append(collisions, p) }, nil)

	for _, body := range []string{
		`scalar = "a"` + "\n[m]\nk1 = \"first\"",
		`scalar = "b"` + "\n[m]\nk1 = \"second\"\nk2 = \"added\"",
		`scalar = "c"`,
	} {
		src, md := decodeFixture(t, body)
		mergeWalk(&dst, &src, md, policy)
	}

	if dst.Scalar != "a" {
		t.Errorf("Scalar = %q, want a (first source wins)", dst.Scalar)
	}
	wantMap := map[string]string{"k1": "first", "k2": "added"}
	if !reflect.DeepEqual(dst.M, wantMap) {
		t.Errorf("M = %v, want %v", dst.M, wantMap)
	}
	// scalar collided on sources 2 and 3; the map key k1 collided on source 2.
	wantCollisions := []string{"scalar", "m.k1", "scalar"}
	if !stringsEqual(collisions, wantCollisions) {
		t.Errorf("collisions = %v, want %v", collisions, wantCollisions)
	}
}

func TestMergeWalkFirstWinsSeededClaims(t *testing.T) {
	// Seeding the ledger models "the main file already defined it": the include's
	// value is dropped with a collision even though dst arrives via the walker.
	dst := mwFixture{Scalar: "main"}
	var collisions []string
	policy := includePolicy(func(p string) { collisions = append(collisions, p) }, nil)
	policy.claim("scalar")

	src, md := decodeFixture(t, `scalar = "include"`)
	mergeWalk(&dst, &src, md, policy)

	if dst.Scalar != "main" {
		t.Errorf("Scalar = %q, want main (seeded claim wins)", dst.Scalar)
	}
	if !stringsEqual(collisions, []string{"scalar"}) {
		t.Errorf("collisions = %v, want [scalar]", collisions)
	}
}

// --- untagged default + include whitelist -------------------------------

func TestMergeWalkUntaggedDefaultsAndWhitelist(t *testing.T) {
	// Overlay: an untagged field defaults to last-wins whole-replace.
	dst, _, _ := runMerge(t, `untagged = "old"`, `untagged = "new"`, false)
	if dst.Untagged != "new" {
		t.Errorf("Untagged = %q, want new (untagged default is replace)", dst.Untagged)
	}

	// Include: a field with no include: tag is reported as not whitelisted and
	// left untouched.
	got, _, notWhitelisted := runMerge(t, `untagged = "old"`, `untagged = "new"`, true)
	if got.Untagged != "old" {
		t.Errorf("Untagged = %q, want old (not whitelisted, not merged)", got.Untagged)
	}
	if !stringsEqual(notWhitelisted, []string{"untagged"}) {
		t.Errorf("notWhitelisted = %v, want [untagged]", notWhitelisted)
	}

	// A not-whitelisted field the include never sets is not reported.
	_, _, quiet := runMerge(t, `untagged = "old"`, `scalar = "x"`, true)
	if len(quiet) != 0 {
		t.Errorf("notWhitelisted = %v, want none when untouched", quiet)
	}
}

// --- generic deep clone --------------------------------------------------

type mwCloneable struct {
	Ptr        *int
	Slice      []string
	Map        map[string]int
	Nested     mwSub
	NestedPtr  *mwSub
	MapOfSlice map[string][]int
	SliceOfPtr []*int
}

func TestDeepCloneNoAliasing(t *testing.T) {
	n := 7
	m := 3
	src := mwCloneable{
		Ptr:        &n,
		Slice:      []string{"a", "b"},
		Map:        map[string]int{"k": 1},
		Nested:     mwSub{A: "na", B: "nb"},
		NestedPtr:  &mwSub{A: "pa"},
		MapOfSlice: map[string][]int{"x": {1, 2}},
		SliceOfPtr: []*int{&m},
	}

	clone := deepClone(src)

	// Every reference must be independent.
	*clone.Ptr = 99
	clone.Slice[0] = "MUT"
	clone.Slice = append(clone.Slice, "c")
	clone.Map["k"] = 99
	clone.Map["new"] = 1
	clone.Nested.A = "MUT"
	clone.NestedPtr.A = "MUT"
	clone.MapOfSlice["x"][0] = 99
	*clone.SliceOfPtr[0] = 99

	if *src.Ptr != 7 {
		t.Errorf("src.Ptr aliased: %d", *src.Ptr)
	}
	if !reflect.DeepEqual(src.Slice, []string{"a", "b"}) {
		t.Errorf("src.Slice aliased: %v", src.Slice)
	}
	if !reflect.DeepEqual(src.Map, map[string]int{"k": 1}) {
		t.Errorf("src.Map aliased: %v", src.Map)
	}
	if src.Nested.A != "na" {
		t.Errorf("src.Nested aliased: %v", src.Nested)
	}
	if src.NestedPtr.A != "pa" {
		t.Errorf("src.NestedPtr aliased: %v", *src.NestedPtr)
	}
	if !reflect.DeepEqual(src.MapOfSlice["x"], []int{1, 2}) {
		t.Errorf("src.MapOfSlice aliased: %v", src.MapOfSlice["x"])
	}
	if *src.SliceOfPtr[0] != 3 {
		t.Errorf("src.SliceOfPtr aliased: %d", *src.SliceOfPtr[0])
	}

	// A nil-bearing clone round-trips without allocating.
	if got := deepClone(mwCloneable{}); got.Ptr != nil || got.Slice != nil || got.Map != nil {
		t.Errorf("zero clone allocated references: %+v", got)
	}
}

// --- drift check ---------------------------------------------------------

type driftBad struct {
	Unknown  string   `toml:"unknown" merge:"bogus"`   // unknown kind
	Mismatch string   `toml:"mismatch" merge:"append"` // append needs a slice
	OK       []string `toml:"ok" merge:"append"`       // legal, no problem
}

func TestCheckMergeTagsFlagsUnknownAndMismatch(t *testing.T) {
	problems := checkMergeTags(reflect.TypeOf(driftBad{}))
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want exactly 2", problems)
	}
	if !containsSubstr(problems, "unknown kind") {
		t.Errorf("problems %v missing unknown-kind report", problems)
	}
	if !containsSubstr(problems, "needs a slice") {
		t.Errorf("problems %v missing type-mismatch report", problems)
	}
}

func TestCheckMergeTagsCleanFixtureHasNoProblems(t *testing.T) {
	if problems := checkMergeTags(reflect.TypeOf(mwFixture{})); len(problems) != 0 {
		t.Fatalf("clean fixture reported problems: %v", problems)
	}
}

func TestCheckMergeTagsMalformedListByKey(t *testing.T) {
	type bad struct {
		Benches []mwBench `toml:"benches" merge:"list-by-key"` // missing =<field>
	}
	problems := checkMergeTags(reflect.TypeOf(bad{}))
	if len(problems) != 1 || !containsSubstr(problems, "malformed list-by-key") {
		t.Fatalf("problems = %v, want one malformed-list-by-key report", problems)
	}

	type badKeyField struct {
		Benches []mwBench `toml:"benches" merge:"list-by-key=nope"` // field not on element
	}
	problems = checkMergeTags(reflect.TypeOf(badKeyField{}))
	if len(problems) != 1 || !containsSubstr(problems, "key field") {
		t.Fatalf("problems = %v, want one missing-key-field report", problems)
	}
}

// --- helpers -------------------------------------------------------------

func stringsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func containsSubstr(items []string, sub string) bool {
	for _, s := range items {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
