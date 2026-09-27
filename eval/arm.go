package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/tasks"
)

// What a Bare arm is given to implement. A Ticket arm gets the request as the
// human first wrote it, so its gap to the Bare arm is what planning added.
const (
	armInputSpec   = "spec"
	armInputTicket = "ticket"
)

type armFile struct {
	Kind     string         `toml:"kind"`
	Agent    string         `toml:"agent"`
	Model    string         `toml:"model"`
	Input    string         `toml:"input"`
	Config   map[string]any `toml:"config"`
	Manifest map[string]any `toml:"manifest"`
}

func loadArm(root, name string) (armFile, error) {
	var arm armFile
	if !namePattern.MatchString(name) {
		return arm, fmt.Errorf("invalid Arm name %q", name)
	}
	metadata, err := toml.DecodeFile(filepath.Join(root, name+".toml"), &arm)
	if err != nil {
		return arm, fmt.Errorf("read Arm: %w", err)
	}
	for _, key := range metadata.Undecoded() {
		if key[0] != "config" && key[0] != "manifest" {
			return arm, fmt.Errorf("unknown Arm field: %s", key)
		}
	}
	if arm.Kind != "bare" && arm.Kind != "pop" {
		return arm, fmt.Errorf("Arm kind must be bare or pop")
	}
	if strings.TrimSpace(arm.Agent) == "" || strings.TrimSpace(arm.Model) == "" {
		return arm, fmt.Errorf("Arm requires agent and model")
	}
	if arm.Kind == "bare" && (len(arm.Config) != 0 || len(arm.Manifest) != 0) {
		return arm, fmt.Errorf("Bare arm cannot have Pop overrides")
	}
	if arm.Input == "" {
		arm.Input = armInputSpec
	}
	if arm.Input != armInputSpec && arm.Input != armInputTicket {
		return arm, fmt.Errorf("Arm input must be spec or ticket")
	}
	if arm.Kind == "pop" && arm.Input == armInputTicket {
		return arm, fmt.Errorf("Pop arm cannot take the ticket: it drains the Case's Task split")
	}
	invocation, err := tasks.ResolveAgentInvocation(arm.agentSpec(), "", "", ".")
	if err != nil {
		return arm, fmt.Errorf("resolve Arm agent: %w", err)
	}
	if invocation.OutputFormat == tasks.AgentOutputPlain {
		return arm, fmt.Errorf("Arm requires a captured Agent preset")
	}
	if invocation.PinnedModel() != arm.Model {
		return arm, fmt.Errorf("Arm agent conflicts with model %q", arm.Model)
	}
	return arm, nil
}

func (a armFile) agentSpec() string {
	return a.Agent + " --model " + strconv.Quote(a.Model)
}
