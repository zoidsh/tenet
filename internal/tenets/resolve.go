package tenets

import (
	"errors"
	"fmt"
	"sort"
)

// resolve turns what the file named into the tenets that will run: the
// presets in the order they are listed, then the rules, then the local
// tenets, then the disables, then the overrides. A local tenet that carries a
// built-in id replaces the built-in where it stood, so that moving a rule
// into the config does not reorder the report.
func (c *Config) resolve() error {
	var out []*Tenet
	at := map[string]int{}

	for _, name := range c.Presets {
		preset, err := BuiltinPreset(name)
		if err != nil {
			return fmt.Errorf("presets: %w", err)
		}
		for _, id := range preset.Rules {
			if _, ok := at[id]; ok {
				continue
			}
			rule, err := BuiltinRule(id)
			if err != nil {
				return fmt.Errorf("presets: %q: %w", name, err)
			}
			rule.Tenet.Origin = name
			at[id] = len(out)
			out = append(out, rule.Tenet)
		}
	}

	for _, id := range c.Rules {
		if i, ok := at[id]; ok {
			return fmt.Errorf("rules: %q is already in the config, from %s", id, origin(out[i]))
		}
		rule, err := BuiltinRule(id)
		if err != nil {
			return fmt.Errorf("rules: %w", err)
		}
		rule.Tenet.Origin = OriginRules
		at[id] = len(out)
		out = append(out, rule.Tenet)
	}

	for _, t := range c.Tenets {
		t.Origin = OriginLocal
		if i, ok := at[t.ID]; ok {
			out[i] = t
			continue
		}
		at[t.ID] = len(out)
		out = append(out, t)
	}

	for _, id := range c.Disable {
		i, ok := at[id]
		if !ok {
			return fmt.Errorf("disable: no tenet %q in the config", id)
		}
		out[i] = nil
		delete(at, id)
	}
	kept := out[:0]
	for _, t := range out {
		if t != nil {
			kept = append(kept, t)
		}
	}
	out = kept

	byID := make(map[string]*Tenet, len(out))
	for _, t := range out {
		byID[t.ID] = t
	}
	for _, id := range sortedKeys(c.Override) {
		t, ok := byID[id]
		if !ok {
			return fmt.Errorf("override: no tenet %q in the config", id)
		}
		c.Override[id].applyTo(t)
	}

	if len(out) == 0 {
		return errors.New("tenets: at least one tenet is required; name a preset, a rule or a tenet of your own")
	}
	c.Tenets = out
	for _, t := range c.Tenets {
		t.model = c.Model
	}
	return nil
}

// origin names where a tenet in the resolved set came from, for the error
// that says an id arrived twice.
func origin(t *Tenet) string {
	if t.Origin == OriginRules || t.Origin == OriginLocal {
		return t.Origin
	}
	return fmt.Sprintf("preset %q", t.Origin)
}

func (o *Override) applyTo(t *Tenet) {
	if o.Fail != nil {
		t.Fail = o.Fail
	}
	if o.Include != nil {
		t.Include = o.Include
	}
	if o.Exclude != nil {
		t.Exclude = o.Exclude
	}
	if o.Kind != nil {
		t.Kind = o.Kind
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
