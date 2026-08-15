package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// This file is the read side of the override layer: for each overridable key, what value is in force, which layer produced it, and — when an override
// exists — what the sources below it still say (ADR-0202 decision 12). The
// Config dashboard renders these views; nothing here knows about a terminal.
//
// It answers per layer rather than off the merged *Config on purpose. The merged
// config carries the value but not its provenance, and provenance is the whole
// point of the preview: "override" and "config.toml" render the same list.

// OverrideLayer names the config source that produced a key's effective value.
type OverrideLayer string

const (
	// OverrideLayerOverride is config.override.toml, which outranks every
	// hand-authored source.
	OverrideLayerOverride OverrideLayer = "override"
	// OverrideLayerConfig is the human's own config.toml.
	OverrideLayerConfig OverrideLayer = "config.toml"
	// OverrideLayerInclude is a file config.toml includes; it ranks with the
	// hand-authored file that pulled it in, below an override.
	OverrideLayerInclude OverrideLayer = "config.toml include"
	// OverrideLayerRepoTOML is a committed .pop/config.toml. It defines repo-scope
	// keys only, and its locus is the checkout whose tree it sits in — this
	// worktree, or the one it inherits from.
	OverrideLayerRepoTOML OverrideLayer = ".pop/config.toml"
	// OverrideLayerDefault means no layer defines the key and the built-in
	// default stands.
	OverrideLayerDefault OverrideLayer = "built-in default"
	// OverrideLayerFallthrough means the key resolves to nothing of its own and
	// resolution walks on to the key named in Locus.
	OverrideLayerFallthrough OverrideLayer = "fallthrough"
)

// overrideFallthrough is the documented fallthrough of one overridable key: the key
// resolution walks on to when this one resolves empty, and how a preview says so
// in words.
type overrideFallthrough struct {
	Key    string
	Phrase string
}

// overrideFallthroughs records which keys fall through when empty. Verify
// says so in its own schema comment (VerifyConfig.Agents) and routine implements
// it in routine/agents.go; implement and attended have nowhere to walk on to.
//
// The second half of ADR-0202 decision 6 — that an override of `agents = []`
// *disables* the fallthrough — is what the preview words below promise. The
// editor stores that emptiness as a real value and the ladder read here tells it
// from an absent key, so the preview is right about what is stored. Agent
// resolution is not: resolveVerifier (tasks/verify.go) and
// ResolveRoutineAgentPresets (routine/agents.go) still walk on from any empty
// list, whoever wrote it. Closing that means giving both run paths a way to ask
// who wrote the emptiness, which is a change to them and not to this file.
var overrideFallthroughs = map[string]overrideFallthrough{
	"work.verify.agents":  {Key: "work.implement.agents", Phrase: "the implement list"},
	"work.routine.agents": {Key: "work.implement.agents", Phrase: "the implement list"},
}

// Wording for the two states that both look empty (ADR-0202 decision 6). They
// are the reason the preview carries a note at all: an empty list and an absent
// value render identically, and only one of them disables the fallthrough.
const (
	overrideNoteFallsThrough  = "no override — falls through to %s"
	overrideNoteEmptyOverride = "override set to an empty list — fallthrough disabled"
)

// OverrideKeyView is one overridable key as a reader meets it: the row text
// on the left of the Config dashboard, and everything its preview shows.
type OverrideKeyView struct {
	// Key is the dotted config path, the spelling `pop config keys` prints.
	Key string
	// Desc is the key's one-line schema description.
	Desc string
	// Overridden reports that config.override.toml carries this key, which is
	// what the list marks and what gives the copy and remove actions a target.
	Overridden bool
	// Layer is the source that produced the effective value.
	Layer OverrideLayer
	// Locus qualifies Layer: the include file for a hand-authored include, or
	// the key walked on to for a fallthrough.
	Locus string
	// EffectiveTOML is the value in force, rendered as one `key = value` TOML
	// statement — the same shape the editor's buffer takes.
	EffectiveTOML string
	// SourceTOML is what the layer below an override still says, rendered the
	// same way. Empty when nothing is overridden, or when no layer below the
	// override defines the key.
	SourceTOML string
	// SourceLayer and SourceLocus name where SourceTOML came from, or — when it
	// is empty — what would produce the value if the override were removed.
	SourceLayer OverrideLayer
	SourceLocus string
	// Note is the sentence that tells two empty-looking states apart, empty when
	// the value speaks for itself.
	Note string
	// Reach is the key's declared reach (ADR-0198), nil for a key that declares
	// none.
	Reach []ConfigKeyReachLine
}

// Provenance is the one-line answer to "where did this value come from".
func (v OverrideKeyView) Provenance() string {
	return overrideProvenance(v.Layer, v.Locus)
}

// SourceProvenance names where the value below an override comes from, so a
// reader knows what removing the override restores.
func (v OverrideKeyView) SourceProvenance() string {
	return overrideProvenance(v.SourceLayer, v.SourceLocus)
}

func overrideProvenance(layer OverrideLayer, locus string) string {
	switch {
	case layer == "":
		return ""
	case layer == OverrideLayerFallthrough:
		return string(layer) + " → " + locus
	case locus != "":
		return string(layer) + " (" + locus + ")"
	default:
		return string(layer)
	}
}

// OverrideKeyViews resolves every overridable key against the layers that
// can define it, in registry order.
func OverrideKeyViews(configPath string) ([]OverrideKeyView, error) {
	return OverrideKeyViewsWith(defaultDeps, configPath)
}

// OverrideKeyViewsWith is the injectable variant.
func OverrideKeyViewsWith(d *Deps, configPath string) ([]OverrideKeyView, error) {
	layers, err := overrideValueLayers(d, configPath)
	if err != nil {
		return nil, err
	}
	types := globalKeyTypes()
	keys := OverrideKeys()
	views := make([]OverrideKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, overrideKeyView(key, types[key.Key], layers))
	}
	return views, nil
}

// overrideValueLayer is one config source a key can be defined in, held as a
// generic TOML document so any overridable key can be read out of it without the
// schema having a say.
type overrideValueLayer struct {
	layer OverrideLayer
	locus string
	doc   map[string]any
}

// overrideValueLayers returns every layer that can define an overridable key, highest
// rank first — the ladder applyConfigLayerMerge builds, read from the top so the
// first layer that defines a key is the one in force. A layer whose file is
// absent contributes an empty document rather than dropping out, so the ladder's
// shape does not depend on which files happen to exist.
func overrideValueLayers(d *Deps, configPath string) ([]overrideValueLayer, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPathWith(d)
	}

	// Only the layer's global half answers here: a key of this list is a dotted
	// config path, and the per-repository blocks are addressed by repository
	// rather than by path (ADR-0212 decisions 3 and 7).
	overrides, err := loadOverrideLayer(d)
	if err != nil {
		return nil, err
	}
	userDoc, err := genericTOMLFile(d, configPath)
	if err != nil {
		return nil, err
	}
	defaultsDoc := map[string]any{}
	if _, err := toml.Decode(embeddedDefaultsTOML, &defaultsDoc); err != nil {
		return nil, fmt.Errorf("embedded defaults: %w", err)
	}

	layers := []overrideValueLayer{
		{layer: OverrideLayerOverride, doc: overrides.globalDoc()},
		{layer: OverrideLayerConfig, doc: userDoc},
	}
	for _, include := range documentIncludes(d, configPath, userDoc) {
		doc, err := genericTOMLFile(d, include)
		if err != nil {
			return nil, err
		}
		layers = append(layers, overrideValueLayer{layer: OverrideLayerInclude, locus: include, doc: doc})
	}
	layers = append(layers, overrideValueLayer{layer: OverrideLayerDefault, doc: defaultsDoc})
	return layers, nil
}

// genericTOMLFile decodes one config file as a generic document. A file that is
// not there decodes to an empty document: the dashboard has to open on a machine
// that has never written a config.toml.
func genericTOMLFile(d *Deps, path string) (map[string]any, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return map[string]any{}, nil
	}
	doc := map[string]any{}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return doc, nil
}

// documentIncludes resolves the includes list off the generic document, the same
// way LoadWith resolves it off the decoded struct: ~ expanded, relative paths
// taken against the config file's own directory.
func documentIncludes(d *Deps, configPath string, doc map[string]any) []string {
	raw, ok := doc["includes"].([]any)
	if !ok {
		return nil
	}
	dir := filepath.Dir(configPath)
	var paths []string
	for _, entry := range raw {
		include, ok := entry.(string)
		if !ok || strings.TrimSpace(include) == "" {
			continue
		}
		expanded := expandHomeWith(d, include)
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(dir, expanded)
		}
		paths = append(paths, expanded)
	}
	return paths
}

// overrideKeyView resolves one key against the layer ladder.
func overrideKeyView(key OverrideKey, tomlType string, layers []overrideValueLayer) OverrideKeyView {
	view := OverrideKeyView{Key: key.Key, Desc: key.Desc}
	if reach, ok := ConfigKeyReachFor(key.Key); ok {
		view.Reach = reach.Lines
	}

	value, idx := topmostValue(key.Key, layers)
	view.Overridden = idx == 0 && layers[0].layer == OverrideLayerOverride
	if idx >= 0 {
		view.EffectiveTOML = renderKeyTOML(key.Key, value)
	} else {
		view.EffectiveTOML = key.Key + " = " + zeroTOMLLiteral(tomlType)
	}

	walkOn, hasFallthrough := overrideFallthroughs[key.Key]
	empty := isEmptyTOMLValue(value)

	switch {
	case hasFallthrough && empty && view.Overridden:
		view.Layer = OverrideLayerOverride
		view.Note = overrideNoteEmptyOverride
	case hasFallthrough && empty:
		view.Layer = OverrideLayerFallthrough
		view.Locus = walkOn.Key
		view.Note = fmt.Sprintf(overrideNoteFallsThrough, walkOn.Phrase)
	case idx >= 0:
		view.Layer = layers[idx].layer
		view.Locus = layers[idx].locus
	default:
		view.Layer = OverrideLayerDefault
	}

	if view.Overridden {
		sourceValue, sourceIdx := topmostValue(key.Key, layers[1:])
		switch {
		case sourceIdx >= 0:
			source := layers[1+sourceIdx]
			view.SourceTOML = renderKeyTOML(key.Key, sourceValue)
			view.SourceLayer = source.layer
			view.SourceLocus = source.locus
		case hasFallthrough:
			view.SourceLayer = OverrideLayerFallthrough
			view.SourceLocus = walkOn.Key
		default:
			view.SourceLayer = OverrideLayerDefault
		}
	}
	return view
}

// topmostValue returns the value of the highest-ranked layer that defines key,
// and that layer's index, or (nil, -1) when no layer defines it.
func topmostValue(key string, layers []overrideValueLayer) (any, int) {
	for i, layer := range layers {
		if value, ok := documentValue(layer.doc, key); ok {
			return value, i
		}
	}
	return nil, -1
}

// globalKeyTypes maps every key of the global surface to its TOML type, so a key
// no layer defines still renders as config: an unset list reads `= []`, not as a
// blank the reader has to interpret.
func globalKeyTypes() map[string]string {
	docs, _ := ScopeKeyDocsRecursive(ScopeGlobal)
	types := make(map[string]string, len(docs))
	for _, doc := range docs {
		types[doc.Key] = doc.Type
	}
	return types
}

func zeroTOMLLiteral(tomlType string) string {
	switch tomlType {
	case "array", "array of tables":
		return "[]"
	case "table":
		return "{}"
	case "boolean":
		return "false"
	case "integer":
		return "0"
	case "float":
		return "0.0"
	default:
		return `""`
	}
}

// isEmptyTOMLValue reports whether a value carries nothing — the state that makes
// a key with a documented fallthrough walk on.
func isEmptyTOMLValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case []map[string]any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

// renderKeyTOML renders one whole key as the TOML statement that would set it.
// The preview is config format throughout and never prose (ADR-0202 decision
// 12), and this is the same `key = value` line the editor hands to $EDITOR.
func renderKeyTOML(key string, value any) string {
	return key + " = " + renderTOMLValue(value, "")
}

// renderTOMLValue renders a generic TOML value. Arrays break one element per
// line — an agent list is read down the page, and its order is the fallback
// order — while tables stay inline so an entry occupies one line.
func renderTOMLValue(value any, indent string) string {
	switch v := value.(type) {
	case nil:
		return `""`
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case time.Time:
		return v.Format(time.RFC3339)
	case []any:
		return renderTOMLArray(v, indent)
	// An array of tables written as [[work.verify.agents]] blocks decodes to a
	// typed slice rather than the generic one, and it is the same array: the
	// preview shows one entry per line either way.
	case []map[string]any:
		elems := make([]any, 0, len(v))
		for _, elem := range v {
			elems = append(elems, elem)
		}
		return renderTOMLArray(elems, indent)
	case map[string]any:
		if len(v) == 0 {
			return "{}"
		}
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, name+" = "+renderTOMLValue(v[name], indent))
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func renderTOMLArray(elems []any, indent string) string {
	if len(elems) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[\n")
	for _, elem := range elems {
		b.WriteString(indent + "  ")
		b.WriteString(renderTOMLValue(elem, indent+"  "))
		b.WriteString(",\n")
	}
	b.WriteString(indent + "]")
	return b.String()
}
