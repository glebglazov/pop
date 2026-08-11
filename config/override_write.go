package config

// This file is the write side of config.override.toml, the pop-written layer
// that outranks every hand-authored source (ADR-0202). It is a library only:
// the editor component that drives it is the sole writer pop ships, and it
// lands separately, so no command reaches this code yet.
//
// Every entry point works on one whole key: the unit of an override is a key's
// entire value as TOML, never a patch of it (ADR-0202 decision 2). Keys are
// dotted config paths — the same spelling `pop config keys` prints, e.g.
// "work.implement.agents".

// OverrideValue returns the whole value config.override.toml stores for a dotted
// config key, decoded as a generic TOML value. The bool is false when the file
// or the key is absent — "no override here", which is a different state from an
// override deliberately set to an empty value (ADR-0202 decision 6).
func OverrideValue(key string) (any, bool, error) {
	return OverrideValueWith(defaultDeps, key)
}

// OverrideValueWith is the injectable variant.
func OverrideValueWith(d *Deps, key string) (any, bool, error) {
	doc, _, err := overrideConfigFile(d).load(d)
	if err != nil {
		return nil, false, err
	}
	value, ok := documentValue(doc, key)
	return value, ok, nil
}

// SetOverrideValue records value as the whole value of a dotted config key, so
// it beats whatever the hand-authored sources say for that key at the next
// config load. Other overrides in the file are left alone and the write is
// atomic.
func SetOverrideValue(key string, value any) error {
	return SetOverrideValueWith(defaultDeps, key, value)
}

// SetOverrideValueWith is the injectable variant.
func SetOverrideValueWith(d *Deps, key string, value any) error {
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	if err := setDocumentValue(doc, key, value); err != nil {
		return err
	}
	return file.save(d, doc)
}

// DeleteOverrideValue removes a dotted config key from config.override.toml, so
// the hand-authored value below it comes back into force. Deleting a key that
// carries no override is a no-op. Tables the removal empties are pruned, and the
// file itself is deleted once its last key goes.
func DeleteOverrideValue(key string) error {
	return DeleteOverrideValueWith(defaultDeps, key)
}

// DeleteOverrideValueWith is the injectable variant.
func DeleteOverrideValueWith(d *Deps, key string) error {
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	if !deleteDocumentValue(doc, key) {
		return nil
	}
	return file.save(d, doc)
}
