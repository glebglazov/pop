package config

import "sync"

// ConfigKeyReach is a runtime answer about one config key: per-actor lines
// saying what shape the key takes for that actor, or that actor's own stated
// reason it takes none (ADR-0198). It is declared against the key, not against
// a command, and is separate from the reflected schema — a key that declares
// none is absent from the registry and renders exactly as it did before.
type ConfigKeyReach struct {
	Lines []ConfigKeyReachLine
}

// ConfigKeyReachLine is one actor's answer for a key.
type ConfigKeyReachLine struct {
	Actor  string
	Detail string // argv shape when the key reaches the actor; otherwise the reason it does not
}

// configKeyReaches holds reaches registered from packages above config.
//
// The inversion exists because config owns the key catalog but sits below the
// packages that know which actors a key can touch (agent adapters live in
// tasks/, which already imports config). config cannot import those packages,
// so they push a reach in at init rather than config reading down. The same
// RegisterConfigKeyReach call is what a second, non-agent key would use.
var (
	configKeyReachMu sync.RWMutex
	configKeyReaches = map[string]ConfigKeyReach{}
)

// RegisterConfigKeyReach records reach for key. A later call replaces the
// previous declaration. Callers above config register at init; tests may also
// register a temporary key and ClearConfigKeyReach it in cleanup.
func RegisterConfigKeyReach(key string, reach ConfigKeyReach) {
	configKeyReachMu.Lock()
	defer configKeyReachMu.Unlock()
	configKeyReaches[key] = reach
}

// ClearConfigKeyReach removes any declared reach for key, restoring the
// "declares none" state. Used by tests that need a key without reach.
func ClearConfigKeyReach(key string) {
	configKeyReachMu.Lock()
	defer configKeyReachMu.Unlock()
	delete(configKeyReaches, key)
}

// ConfigKeyReachFor returns the reach declared for key, if any.
func ConfigKeyReachFor(key string) (ConfigKeyReach, bool) {
	configKeyReachMu.RLock()
	defer configKeyReachMu.RUnlock()
	reach, ok := configKeyReaches[key]
	if !ok {
		return ConfigKeyReach{}, false
	}
	out := ConfigKeyReach{Lines: append([]ConfigKeyReachLine(nil), reach.Lines...)}
	return out, true
}
