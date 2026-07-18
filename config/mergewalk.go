package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the type-generic config merge engine (ADR-0122): one reflection
// walker that merges same-type struct pairs field-by-field, one generic deep
// clone that replaces the hand-written clones, and a drift check that validates
// the granularity tags. It is deliberately not yet wired into any production
// merge path — the layer-overlay, include, and repo-scope surfaces still run
// their bespoke code and are rewired by later slices.
//
// Granularity is declared by two struct tags. `merge:"<kind>"` drives the
// overlay and repo-scope walks; `include:"<kind>"` drives the include walk, and
// its mere presence is the ADR-0037 whitelist (a field without an include: tag
// never participates in an include). The kinds:
//
//	replace           whole-value last-wins (the untagged default)
//	fields            descend into a sub-struct, recursing on its own tags
//	map               per-key merge honouring the policy's collision mode
//	map-first-wins    per-key merge, first source's key always wins
//	append            concatenate the source slice onto the destination
//	list-by-key=<f>   keyed union of a slice, collisions resolved by policy
//
// WHO wins is the caller's policy, never the engine's: overlay is last-wins
// silent, include is first-wins with a warning callback, repo-scope is a
// last-wins ladder. The engine executes ADR-0037/0083/0092 precedence; it
// decides nothing.

const (
	mergeTagName   = "merge"
	includeTagName = "include"
)

const (
	kindReplace      = "replace"
	kindFields       = "fields"
	kindMap          = "map"
	kindMapFirstWins = "map-first-wins"
	kindAppend       = "append"
	kindListByKey    = "list-by-key"
)

// knownKinds is the closed set of legal granularity kinds; the drift check flags
// anything else.
var knownKinds = map[string]bool{
	kindReplace:      true,
	kindFields:       true,
	kindMap:          true,
	kindMapFirstWins: true,
	kindAppend:       true,
	kindListByKey:    true,
}

// mergeMode is a policy's collision behaviour when both destination and source
// define the same field.
type mergeMode int

const (
	// lastWins overwrites the destination with every present source field,
	// silently. The overlay and repo-scope ladders use it: the later source wins.
	lastWins mergeMode = iota
	// firstWins keeps the earliest source's value and reports a collision when a
	// later source also defines a claimed field. Includes use it (ADR-0037
	// parent-first precedence).
	firstWins
)

// mergeKind is a parsed granularity tag value.
type mergeKind struct {
	kind     string // one of the kind* constants (or an unknown string)
	keyField string // list-by-key's toml key field; "" unless kind == kindListByKey
}

// parseKind parses a tag value into a mergeKind. An empty value is the untagged
// default of whole-value replace. `list-by-key=<field>` splits off the key
// field; a bare `list-by-key` yields an empty keyField, which the drift check
// flags as malformed.
func parseKind(tag string) mergeKind {
	switch {
	case tag == "":
		return mergeKind{kind: kindReplace}
	case tag == kindListByKey:
		return mergeKind{kind: kindListByKey} // malformed: no =<field>
	case strings.HasPrefix(tag, kindListByKey+"="):
		return mergeKind{kind: kindListByKey, keyField: tag[len(kindListByKey)+1:]}
	default:
		return mergeKind{kind: tag}
	}
}

// mergePolicy governs one merge walk: which granularity tag it reads, how it
// resolves collisions, the claimed-field ledger threaded across successive
// sources (so several include files merge parent-first correctly), and the
// caller-supplied callbacks. The callbacks receive a dotted toml key path and
// build their own message text — the engine ships no strings.
type mergePolicy struct {
	tag              string          // mergeTagName or includeTagName
	mode             mergeMode       // lastWins or firstWins
	claimed          map[string]bool // key paths an earlier source already set
	onCollision      func(keyPath string)
	onNotWhitelisted func(keyPath string) // include only: field lacks an include: tag
}

// overlayPolicy reads the merge: tag and lets the later source win silently.
func overlayPolicy() *mergePolicy {
	return &mergePolicy{tag: mergeTagName, mode: lastWins}
}

// repoScopePolicy reads the merge: tag and lets the later ladder source win
// silently. It is behaviourally identical to overlayPolicy; the ladder ordering
// lives in the caller's source enumeration, not here.
func repoScopePolicy() *mergePolicy {
	return &mergePolicy{tag: mergeTagName, mode: lastWins}
}

// includePolicy reads the include: tag with first-wins precedence. onCollision
// fires when a later source redefines a claimed field; onNotWhitelisted fires
// for a field without an include: tag. Either callback may be nil.
func includePolicy(onCollision, onNotWhitelisted func(keyPath string)) *mergePolicy {
	return &mergePolicy{
		tag:              includeTagName,
		mode:             firstWins,
		claimed:          map[string]bool{},
		onCollision:      onCollision,
		onNotWhitelisted: onNotWhitelisted,
	}
}

func (p *mergePolicy) isClaimed(path string) bool {
	return p.claimed[path]
}

func (p *mergePolicy) claim(path string) {
	if p.claimed == nil {
		p.claimed = map[string]bool{}
	}
	p.claimed[path] = true
}

// mergeWalk merges src into dst in place, field by field, under policy. dst and
// src must be non-nil pointers to the same struct type. Presence comes from md:
// only fields the source's toml.MetaData reports as defined are merged, so a
// defined-but-zero value still overrides while an absent one leaves dst intact.
func mergeWalk[T any](dst, src *T, md toml.MetaData, policy *mergePolicy) {
	if dst == nil || src == nil {
		return
	}
	mergeStruct(reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem(), md, nil, policy)
}

// mergeStruct merges every toml-tagged field of two same-type structs. prefix is
// the dotted toml path of this struct within the root (nil at the root).
// Anonymous embedded structs are flattened at the parent's prefix, matching how
// keys.go derives key paths.
func mergeStruct(dst, src reflect.Value, md toml.MetaData, prefix []string, policy *mergePolicy) {
	t := dst.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			mergeStruct(dst.Field(i), src.Field(i), md, prefix, policy)
			continue
		}
		name := tomlName(sf)
		if name == "" || name == "-" {
			continue // never decoded (e.g. the toml:"-" sentinel)
		}
		path := childPath(prefix, name)

		tagVal, hasTag := sf.Tag.Lookup(policy.tag)
		if policy.tag == includeTagName && !hasTag {
			// ADR-0037: no include: tag means the field is not whitelisted for
			// includes, so a source that defines it is reported and ignored rather
			// than silently leaking the value in.
			if md.IsDefined(path...) && policy.onNotWhitelisted != nil {
				policy.onNotWhitelisted(strings.Join(path, "."))
			}
			continue
		}
		mergeField(dst.Field(i), src.Field(i), md, path, parseKind(tagVal), policy)
	}
}

// mergeField merges one field according to its kind, gated on the source having
// defined the key. Unknown kinds fall through to whole-value replace — the drift
// check is responsible for catching them; the runtime merge stays safe.
func mergeField(dst, src reflect.Value, md toml.MetaData, path []string, kind mergeKind, policy *mergePolicy) {
	if !md.IsDefined(path...) {
		return
	}
	switch kind.kind {
	case kindFields:
		mergeFieldsKind(dst, src, md, path, policy)
	case kindMap:
		mergeMapKind(dst, src, path, policy, false)
	case kindMapFirstWins:
		mergeMapKind(dst, src, path, policy, true)
	case kindAppend:
		mergeAppendKind(dst, src)
	case kindListByKey:
		mergeListByKey(dst, src, path, kind.keyField, policy)
	default: // replace and any unknown kind
		mergeReplace(dst, src, path, policy)
	}
}

// mergeReplace overwrites dst with a deep clone of src, honouring first-wins
// claim tracking.
func mergeReplace(dst, src reflect.Value, path []string, policy *mergePolicy) {
	if policy.mode == firstWins {
		key := strings.Join(path, ".")
		if policy.isClaimed(key) {
			if policy.onCollision != nil {
				policy.onCollision(key)
			}
			return
		}
		dst.Set(deepCloneValue(src))
		policy.claim(key)
		return
	}
	dst.Set(deepCloneValue(src))
}

// mergeFieldsKind descends into a sub-struct, recursing on the nested fields'
// own tags. A pointer destination is allocated on demand so include-only nested
// tables have somewhere to land.
func mergeFieldsKind(dst, src reflect.Value, md toml.MetaData, path []string, policy *mergePolicy) {
	if dst.Kind() == reflect.Ptr {
		if src.IsNil() {
			return
		}
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		mergeStruct(dst.Elem(), src.Elem(), md, path, policy)
		return
	}
	mergeStruct(dst, src, md, path, policy)
}

// mergeMapKind merges a map per key. With forceFirstWins (the map-first-wins
// kind) the first source's key always wins regardless of the policy mode;
// otherwise the policy's collision mode decides, so a last-wins overlay
// overwrites while a first-wins include keeps the earliest. The destination map
// is allocated on demand, and keys present only in dst are preserved.
func mergeMapKind(dst, src reflect.Value, path []string, policy *mergePolicy, forceFirstWins bool) {
	if src.Kind() != reflect.Map || src.IsNil() || src.Len() == 0 {
		return
	}
	if dst.IsNil() {
		dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
	}
	keepFirst := forceFirstWins || policy.mode == firstWins
	it := src.MapRange()
	for it.Next() {
		k := it.Key()
		if keepFirst && dst.MapIndex(k).IsValid() {
			if policy.onCollision != nil {
				policy.onCollision(fmt.Sprintf("%s.%v", strings.Join(path, "."), k.Interface()))
			}
			continue
		}
		dst.SetMapIndex(k, deepCloneValue(it.Value()))
	}
}

// mergeAppendKind concatenates the source slice onto the destination. Append is
// mode-independent: both overlay and include concatenate.
func mergeAppendKind(dst, src reflect.Value) {
	if src.Kind() != reflect.Slice || src.IsNil() || src.Len() == 0 {
		return
	}
	if dst.IsNil() {
		dst.Set(deepCloneValue(src))
		return
	}
	dst.Set(reflect.AppendSlice(deepCloneValue(dst), deepCloneValue(src)))
}

// mergeListByKey unions a slice keyed by the toml field keyField. A source
// element whose key is new is appended; a key already present collides, and the
// collision is resolved by policy — first-wins keeps the destination element,
// last-wins replaces it — with a warning either way when onCollision is set.
func mergeListByKey(dst, src reflect.Value, path []string, keyField string, policy *mergePolicy) {
	if keyField == "" || src.Kind() != reflect.Slice || src.IsNil() {
		return
	}
	for j := 0; j < src.Len(); j++ {
		se := src.Index(j)
		fv, ok := fieldByTomlName(se, keyField)
		if !ok {
			continue
		}
		key := fmt.Sprint(fv.Interface())
		if idx := indexByKey(dst, keyField, key); idx >= 0 {
			if policy.onCollision != nil {
				policy.onCollision(fmt.Sprintf("%s[%s]", strings.Join(path, "."), key))
			}
			if policy.mode != firstWins {
				dst.Index(idx).Set(deepCloneValue(se))
			}
			continue
		}
		dst.Set(reflect.Append(dst, deepCloneValue(se)))
	}
}

// childPath appends a segment to a path without aliasing the parent's backing
// array, so sibling fields never clobber each other's paths.
func childPath(prefix []string, name string) []string {
	out := make([]string, len(prefix)+1)
	copy(out, prefix)
	out[len(prefix)] = name
	return out
}

// fieldByTomlName returns the value of the field whose top-level toml key is
// tomlKey, dereferencing pointers. The bool is false for a non-struct or a
// missing key.
func fieldByTomlName(v reflect.Value, tomlKey string) (reflect.Value, bool) {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if tomlName(t.Field(i)) == tomlKey {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// indexByKey returns the position of the first element in list whose keyField
// stringifies to want, or -1.
func indexByKey(list reflect.Value, keyField, want string) int {
	for i := 0; i < list.Len(); i++ {
		if fv, ok := fieldByTomlName(list.Index(i), keyField); ok {
			if fmt.Sprint(fv.Interface()) == want {
				return i
			}
		}
	}
	return -1
}

// deepClone returns a deep copy of v: pointers, slices, maps, arrays, nested
// structs, and interface values are all copied, so mutating the result never
// aliases v. It is the single replacement for the hand-written clone functions.
func deepClone[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	return deepCloneValue(rv).Interface().(T)
}

// deepCloneValue returns a fresh reflect.Value that deep-copies src. Structs are
// shallow-copied first so unexported fields are preserved, then every settable
// reference-bearing field is deep-cloned over the top.
func deepCloneValue(src reflect.Value) reflect.Value {
	t := src.Type()
	switch src.Kind() {
	case reflect.Ptr:
		if src.IsNil() {
			return reflect.Zero(t)
		}
		out := reflect.New(t.Elem())
		out.Elem().Set(deepCloneValue(src.Elem()))
		return out
	case reflect.Slice:
		if src.IsNil() {
			return reflect.Zero(t)
		}
		out := reflect.MakeSlice(t, src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			out.Index(i).Set(deepCloneValue(src.Index(i)))
		}
		return out
	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(t)
		}
		out := reflect.MakeMapWithSize(t, src.Len())
		it := src.MapRange()
		for it.Next() {
			out.SetMapIndex(deepCloneValue(it.Key()), deepCloneValue(it.Value()))
		}
		return out
	case reflect.Array:
		out := reflect.New(t).Elem()
		for i := 0; i < src.Len(); i++ {
			out.Index(i).Set(deepCloneValue(src.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(t).Elem()
		out.Set(src) // shallow copy first, so unexported fields survive
		for i := 0; i < t.NumField(); i++ {
			if f := out.Field(i); f.CanSet() {
				f.Set(deepCloneValue(src.Field(i)))
			}
		}
		return out
	case reflect.Interface:
		if src.IsNil() {
			return reflect.Zero(t)
		}
		out := reflect.New(t).Elem()
		out.Set(deepCloneValue(src.Elem()))
		return out
	default:
		return src
	}
}

// checkMergeTags walks a tagged struct type by reflection and reports every
// merge:/include: tag that names an unknown kind, has a malformed list-by-key
// spec, or declares a kind the field's Go type cannot support. Later slices call
// it in tests against Config and RepoScopeConfig so a bad tag fails the build's
// test run rather than silently misbehaving at load time.
func checkMergeTags(t reflect.Type) []string {
	var problems []string
	checkMergeTagsRec(t, "", map[reflect.Type]bool{}, &problems)
	return problems
}

func checkMergeTagsRec(t reflect.Type, prefix string, seen map[reflect.Type]bool, problems *[]string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			checkMergeTagsRec(f.Type, prefix, seen, problems)
			continue
		}
		name := tomlName(f)
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		for _, tagName := range []string{mergeTagName, includeTagName} {
			if tagVal, ok := f.Tag.Lookup(tagName); ok {
				checkOneTag(f, key, tagName, tagVal, problems)
			}
		}
		// Descend into nested tables/arrays/maps of structs so the whole tree is
		// validated in one pass.
		if elem, seg := tableElem(f.Type); elem != nil {
			child := key
			if seg != "" {
				child = key + "." + seg
			}
			checkMergeTagsRec(elem, child, seen, problems)
		}
	}
}

// checkOneTag validates a single tag value against the field's type.
func checkOneTag(f reflect.StructField, key, tagName, tagVal string, problems *[]string) {
	kind := parseKind(tagVal)
	if !knownKinds[kind.kind] {
		*problems = append(*problems, fmt.Sprintf("%s: %s:%q unknown kind %q", key, tagName, tagVal, kind.kind))
		return
	}
	base := f.Type
	for base.Kind() == reflect.Ptr {
		base = base.Elem()
	}
	switch kind.kind {
	case kindFields:
		if base.Kind() != reflect.Struct {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q needs a struct, got %s", key, tagName, tagVal, base.Kind()))
		}
	case kindMap, kindMapFirstWins:
		if base.Kind() != reflect.Map {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q needs a map, got %s", key, tagName, tagVal, base.Kind()))
		}
	case kindAppend:
		if base.Kind() != reflect.Slice {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q needs a slice, got %s", key, tagName, tagVal, base.Kind()))
		}
	case kindListByKey:
		if kind.keyField == "" {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q malformed list-by-key: missing =<field>", key, tagName, tagVal))
			return
		}
		if base.Kind() != reflect.Slice {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q needs a slice, got %s", key, tagName, tagVal, base.Kind()))
			return
		}
		elem := base.Elem()
		for elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q needs a slice of structs, got slice of %s", key, tagName, tagVal, elem.Kind()))
			return
		}
		if !structHasTomlField(elem, kind.keyField) {
			*problems = append(*problems, fmt.Sprintf("%s: %s:%q key field %q not found on %s", key, tagName, tagVal, kind.keyField, elem.Name()))
		}
	}
}

// structHasTomlField reports whether t has a field whose top-level toml key is
// tomlKey.
func structHasTomlField(t reflect.Type, tomlKey string) bool {
	for i := 0; i < t.NumField(); i++ {
		if tomlName(t.Field(i)) == tomlKey {
			return true
		}
	}
	return false
}
