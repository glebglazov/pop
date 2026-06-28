package config

import (
	"fmt"
	"strings"
	"time"
)

// TopicStepType names one step in the Topic derivation pipeline (ADR 0068).
type TopicStepType string

const (
	TopicStepTruncate TopicStepType = "truncate"
	TopicStepAgent    TopicStepType = "agent"
)

// TopicSetIf gates when a pipeline step may run against @pop_topic_kind.
type TopicSetIf string

const (
	TopicSetIfEmpty       TopicSetIf = "empty"
	TopicSetIfEmptyOrSeed TopicSetIf = "empty_or_seed"
	TopicSetIfAlways      TopicSetIf = "always"
)

// Topic provenance values stored in @pop_topic_kind.
const (
	TopicKindSeed  = "seed"
	TopicKindFinal = "final"
)

// TopicStep is one typed entry in [pane_monitoring] topic_agents.
type TopicStep struct {
	Type    TopicStepType `toml:"type"`
	Command string        `toml:"command"`
	SetIf   TopicSetIf    `toml:"set_if"`
	// Args are appended to the curated argv for agent steps (claude, ollama,
	// cmd:). They are ignored by truncate steps.
	Args []string `toml:"args"`
	// Timeout is an optional per-step timeout in seconds. When zero, the step
	// falls back to the global topic_derivation_timeout.
	Timeout int `toml:"timeout"`
}

// TopicSteps is the ordered topic_agents list. A bare string entry is sugar for
// { type = "agent", command = "<string>", set_if = "empty" }.
type TopicSteps []TopicStep

// DefaultTopicSteps is the implicit pipeline when topic_agents is unset: a
// single truncate step matching today's truncation behaviour (ADR 0068).
func DefaultTopicSteps() TopicSteps {
	return TopicSteps{{Type: TopicStepTruncate, SetIf: TopicSetIfEmpty}}
}

// TopicSetIfAllows reports whether setIf permits running a step against the
// current @pop_topic_kind (unset = "").
func TopicSetIfAllows(setIf TopicSetIf, topicKind string) bool {
	switch setIf {
	case TopicSetIfAlways:
		return true
	case TopicSetIfEmptyOrSeed:
		return topicKind == "" || topicKind == TopicKindSeed
	case TopicSetIfEmpty:
		return topicKind == ""
	default:
		return topicKind == ""
	}
}

// DerivationTimeout returns the effective timeout for this step. If the step
// defines a positive per-step timeout (seconds), it wins; otherwise the global
// timeout is used.
func (s TopicStep) DerivationTimeout(global time.Duration) time.Duration {
	if s.Timeout > 0 {
		return time.Duration(s.Timeout) * time.Second
	}
	return global
}

func (s *TopicStep) applyDefaults() {
	if s.Type == "" && s.Command != "" {
		s.Type = TopicStepAgent
	}
	if s.SetIf == "" {
		switch s.Type {
		case TopicStepTruncate:
			s.SetIf = TopicSetIfEmpty
		default:
			s.SetIf = TopicSetIfEmptyOrSeed
		}
	}
}

func (s *TopicStep) unmarshalFromInterface(v interface{}) error {
	switch val := v.(type) {
	case string:
		cmd := strings.TrimSpace(val)
		if cmd == "" {
			return fmt.Errorf("agent command must be non-empty")
		}
		*s = TopicStep{
			Type:    TopicStepAgent,
			Command: cmd,
			SetIf:   TopicSetIfEmpty,
		}
		return nil
	case map[string]interface{}:
		if raw, ok := val["type"]; ok {
			t, ok := raw.(string)
			if !ok {
				return fmt.Errorf("type must be a string, got %T", raw)
			}
			s.Type = TopicStepType(strings.TrimSpace(t))
		}
		if raw, ok := val["command"]; ok {
			c, ok := raw.(string)
			if !ok {
				return fmt.Errorf("command must be a string, got %T", raw)
			}
			s.Command = strings.TrimSpace(c)
		}
		if raw, ok := val["set_if"]; ok {
			g, ok := raw.(string)
			if !ok {
				return fmt.Errorf("set_if must be a string, got %T", raw)
			}
			s.SetIf = TopicSetIf(strings.TrimSpace(g))
		}
		if raw, ok := val["args"]; ok {
			args, err := parseStringSlice(raw)
			if err != nil {
				return fmt.Errorf("args: %w", err)
			}
			s.Args = args
		}
		if raw, ok := val["timeout"]; ok {
			n, ok := raw.(int64)
			if !ok {
				return fmt.Errorf("timeout must be an integer, got %T", raw)
			}
			s.Timeout = int(n)
		}
		s.applyDefaults()
		if s.Type != TopicStepTruncate && s.Type != TopicStepAgent {
			return fmt.Errorf("unknown type %q", s.Type)
		}
		if s.Type == TopicStepAgent && s.Command == "" {
			return fmt.Errorf("agent step requires command")
		}
		return nil
	default:
		return fmt.Errorf("entry must be a string or table, got %T", v)
	}
}

func parseStringSlice(v interface{}) ([]string, error) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array, got %T", v)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("element %d must be a string, got %T", i, item)
		}
		out = append(out, s)
	}
	return out, nil
}

// UnmarshalTOML decodes topic_agents as a mixed string-or-table array.
func (ts *TopicSteps) UnmarshalTOML(v interface{}) error {
	arr, ok := v.([]interface{})
	if !ok {
		return fmt.Errorf("topic_agents must be an array, got %T", v)
	}
	steps := make(TopicSteps, 0, len(arr))
	for i, item := range arr {
		var step TopicStep
		if err := step.unmarshalFromInterface(item); err != nil {
			return fmt.Errorf("topic_agents[%d]: %w", i, err)
		}
		steps = append(steps, step)
	}
	*ts = steps
	return nil
}
