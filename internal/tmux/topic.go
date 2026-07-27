package tmux

import "strings"

// The Topic and its provenance are pop's own per-pane tmux user-options, the
// single source of truth for a pane's Topic (ADR 0058). These option keys are
// constructed only here — no consumer names them.
const (
	optTopic     = "@pop_topic"
	optTopicKind = "@pop_topic_kind"
)

// TopicState is a pane's Topic, its provenance kind (empty | seed | final), and
// the session that owns it, read in one round-trip (glossary: Topic, Topic
// provenance).
type TopicState struct {
	Session string
	Topic   string
	Kind    string
}

// ReadTopic reads a pane's Topic, provenance, and owning session in one
// display-message call.
func (t *realTmux) ReadTopic(paneID string) (TopicState, error) {
	out, err := t.run.output("display-message", "-p", "-t", paneID,
		"#{session_name}\t"+"#{"+optTopic+"}\t"+"#{"+optTopicKind+"}")
	if err != nil {
		return TopicState{}, err
	}
	parts := strings.SplitN(out, "\t", 3)
	var st TopicState
	st.Session = parts[0]
	if len(parts) >= 2 {
		st.Topic = parts[1]
	}
	if len(parts) == 3 {
		st.Kind = parts[2]
	}
	return st, nil
}

// SetTopic writes a pane's Topic without touching its provenance.
func (t *realTmux) SetTopic(paneID, topic string) error {
	_, err := t.run.output("set-option", "-p", "-t", paneID, optTopic, topic)
	return err
}

// SetTopicWithKind writes a pane's Topic and provenance together (ADR 0068).
func (t *realTmux) SetTopicWithKind(paneID, topic, kind string) error {
	if _, err := t.run.output("set-option", "-p", "-t", paneID, optTopic, topic); err != nil {
		return err
	}
	_, err := t.run.output("set-option", "-p", "-t", paneID, optTopicKind, kind)
	return err
}

// ClearTopic clears a pane's Topic and its provenance.
func (t *realTmux) ClearTopic(paneID string) error {
	return t.SetTopicWithKind(paneID, "", "")
}

// PaneTopics maps every pane id carrying a non-empty Topic to its Topic text,
// across all sessions. Panes with no Topic are omitted. The tab delimiter keeps
// Topics containing spaces intact (pane ids are tab-free).
func (t *realTmux) PaneTopics() (map[string]string, error) {
	out, err := t.run.output("list-panes", "-a", "-F", "#{pane_id}\t#{"+optTopic+"}")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] != "" {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}
