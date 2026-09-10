package config

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// SetupEntry is a sequential command or a group of independent commands.
// A bare command keeps terminal input; a Parallel group never receives it.
type SetupEntry struct {
	Command  string
	Parallel []string
}

// SetupEntries preserves barriers between a Workbench's setup commands and groups.
type SetupEntries []SetupEntry

func (entries *SetupEntries) UnmarshalTOML(v interface{}) error {
	var arr []interface{}
	switch val := v.(type) {
	case []interface{}:
		arr = val
	case []map[string]interface{}:
		for _, item := range val {
			arr = append(arr, item)
		}
	default:
		return fmt.Errorf("before_apply must be an array of command strings or { parallel = [\"command\", ...] } tables, got %T", v)
	}
	decoded := make(SetupEntries, 0, len(arr))
	for i, item := range arr {
		entry, err := decodeSetupEntry(item)
		if err != nil {
			return fmt.Errorf("before_apply[%d]: %w", i, err)
		}
		decoded = append(decoded, entry)
	}
	*entries = decoded
	return nil
}

func decodeSetupEntry(v interface{}) (SetupEntry, error) {
	switch val := v.(type) {
	case string:
		// Preserve existing command lines, including whitespace and empty commands.
		return SetupEntry{Command: val}, nil
	case map[string]interface{}:
		raw, ok := val["parallel"]
		if !ok || len(val) != 1 {
			return SetupEntry{}, fmt.Errorf("expected a table with only parallel = [\"command\", ...]")
		}
		commands, ok := raw.([]interface{})
		if !ok || len(commands) == 0 {
			return SetupEntry{}, fmt.Errorf("parallel must be a non-empty array of command strings")
		}
		group := make([]string, 0, len(commands))
		for i, raw := range commands {
			command, ok := raw.(string)
			if !ok || strings.TrimSpace(command) == "" {
				return SetupEntry{}, fmt.Errorf("parallel[%d] must be a non-empty command string", i)
			}
			group = append(group, command)
		}
		return SetupEntry{Parallel: group}, nil
	default:
		return SetupEntry{}, fmt.Errorf("expected a command string or { parallel = [\"command\", ...] } table, got %T", v)
	}
}

// MarshalTOML keeps the mixed array spelling on config show and subsequent loads.
// The array is spelled here rather than handed to the encoder whole because an
// array of nothing but tables is written as [[before_apply]] blocks, which is
// not a value this key can be assigned. Each entry's commands still go through
// the encoder, so command text is quoted and escaped as TOML requires.
func (entries SetupEntries) MarshalTOML() ([]byte, error) {
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		commands := entry.Parallel
		if commands == nil {
			commands = []string{entry.Command}
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(map[string]interface{}{"commands": commands}); err != nil {
			return nil, err
		}
		_, value, _ := strings.Cut(buf.String(), "= ")
		value = strings.TrimSpace(value)
		if entry.Parallel == nil {
			value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		} else {
			value = "{ parallel = " + value + " }"
		}
		values = append(values, value)
	}
	return []byte("[" + strings.Join(values, ", ") + "]"), nil
}
