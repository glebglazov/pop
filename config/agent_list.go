package config

import (
	"sort"
	"strings"
)

// This file answers the one question the merged config cannot: when a Work
// group's agent list is empty, who wrote the emptiness. The merge keeps a key's
// value and forgets the layer that produced it, and for the three lists below
// the layer is the whole meaning — an absent list walks on to the implement
// list, while an override of `agents = []` states the group has no agents of its
// own and stops resolution there (ADR-0202 decision 6).

// The dotted paths of the Work-group agent lists that fall through to the
// implement list when empty. They are spelled as paths because that is how the
// override layer and the Config dashboard address a key, and the answer here is
// about a layer rather than about a struct field.
const (
	KeyVerifyAgents  = "work.verify.agents"
	KeyReviewAgents  = "work.review.agents"
	KeyRoutineAgents = "work.routine.agents"
)

// fallthroughAgentLists maps each falling-through key to the entries it holds in
// one config document. Implement and attended are absent because they have
// nowhere to walk on to: an empty list there means the built-in default agent,
// whoever wrote it.
var fallthroughAgentLists = map[string]func(*Config) AgentEntries{
	KeyVerifyAgents:  (*Config).VerifyAgentEntries,
	KeyReviewAgents:  (*Config).ReviewAgentEntries,
	KeyRoutineAgents: (*Config).RoutineAgentEntries,
}

// AgentList is one Work group's agent list as a resolver meets it: the commands
// in force, and the answer to who left it carrying none. The two empty states
// are identical in the value and opposite in meaning, so a caller is handed both
// halves at once rather than re-deriving the second from the config layers.
type AgentList struct {
	// Key is the dotted config path the list came from, so a caller refusing an
	// explicit empty can name the key a human would edit.
	Key string
	// Commands are the commands of the entries that decoded cleanly, in
	// configured order.
	Commands []string
	// EmptyOverride reports that config.override.toml states Key as an empty
	// list. That emptiness is a human's instruction written through pop, and it
	// is the one empty state that disables the group's fallthrough.
	EmptyOverride bool
}

// FallsThrough reports that resolution walks on to the group's documented
// fallback: the list carries nothing and no override said it should.
func (l AgentList) FallsThrough() bool {
	return len(l.Commands) == 0 && !l.EmptyOverride
}

// VerifyAgentList returns [work.verify].agents beside who wrote its emptiness.
func (c *Config) VerifyAgentList() AgentList {
	return c.agentList(KeyVerifyAgents)
}

// ReviewAgentList returns [work.review].agents beside who wrote its emptiness.
func (c *Config) ReviewAgentList() AgentList {
	return c.agentList(KeyReviewAgents)
}

// RoutineAgentList returns [work.routine].agents beside who wrote its emptiness.
func (c *Config) RoutineAgentList() AgentList {
	return c.agentList(KeyRoutineAgents)
}

func (c *Config) agentList(key string) AgentList {
	return AgentList{
		Key:           key,
		Commands:      fallthroughAgentLists[key](c).Commands(),
		EmptyOverride: c.emptyAgentOverride(key),
	}
}

// emptyAgentOverride reports whether the override layer states key as an empty
// list. A config that was never loaded through the layer merge — a literal built
// in a test, or the zero value — states nothing, so every list it carries is an
// absence. The receiver may be nil.
func (c *Config) emptyAgentOverride(key string) bool {
	if c == nil {
		return false
	}
	for _, stated := range c.EmptyAgentOverrides {
		if stated == key {
			return true
		}
	}
	return false
}

// emptyAgentListOverrides names every falling-through agent list the override
// layer states as an empty list. It reads the layer as decoded, before the
// merge, because the merge is exactly where the layer is forgotten.
//
// Emptiness is counted in entries and not in commands: a list of one malformed
// entry is a stated list that happens to yield no command, and the preview shows
// it as the non-empty value it is (ADR-0054 keeps the entry and reports a
// finding). Only a list with nothing in it is the state decision 6 is about.
func emptyAgentListOverrides(layer *configLayer) []string {
	if layer == nil || layer.cfg == nil {
		return nil
	}
	var keys []string
	for key, entries := range fallthroughAgentLists {
		if layer.md.IsDefined(strings.Split(key, ".")...) && len(entries(layer.cfg)) == 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
