package tenets_test

import (
	"slices"
	"testing"

	"github.com/zoidsh/tenetlint/internal/tenets"
)

// minExamples is check.DefaultMinExamples, which this package cannot import
// back: a rule under it is not worth measuring, so it is not worth shipping.
const minExamples = 6

func TestBuiltinRulesShip(t *testing.T) {
	rules, err := tenets.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("the corpus is empty")
	}
	for _, rule := range rules {
		t.Run(rule.ID, func(t *testing.T) {
			if rule.Tenet.ID != rule.ID {
				t.Errorf("rule %q holds tenet %q", rule.ID, rule.Tenet.ID)
			}
			var violations, oks int
			for _, e := range rule.Tenet.Examples {
				switch e.Label {
				case tenets.LabelViolation:
					violations++
				case tenets.LabelOK:
					oks++
				}
			}
			if violations+oks < minExamples {
				t.Errorf("%d examples, want at least %d", violations+oks, minExamples)
			}
			if violations == 0 || oks == 0 {
				t.Errorf("%d violation and %d ok examples, want both", violations, oks)
			}
			if len(rule.Presets) == 0 && !slices.Contains(rule.Tenet.Tags, tenets.StandaloneTag) {
				t.Errorf("no preset includes it and it carries no %q tag", tenets.StandaloneTag)
			}
		})
	}
}

func TestBuiltinRuleIsItsOwnCopy(t *testing.T) {
	first, err := tenets.BuiltinRule("comment-why")
	if err != nil {
		t.Fatal(err)
	}
	first.Tenet.Tenet = "edited"
	second, err := tenets.BuiltinRule("comment-why")
	if err != nil {
		t.Fatal(err)
	}
	if second.Tenet.Tenet == "edited" {
		t.Error("two readings of a rule share the tenet, so one config's override edits another's")
	}
}

func TestBuiltinPresetsNameKnownRules(t *testing.T) {
	presets, err := tenets.BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) == 0 {
		t.Fatal("no presets ship")
	}
	for _, p := range presets {
		for _, id := range p.Rules {
			if _, err := tenets.BuiltinRule(id); err != nil {
				t.Errorf("preset %q: %v", p.Name, err)
			}
		}
	}
}

func TestUnknownBuiltins(t *testing.T) {
	if _, err := tenets.BuiltinRule("no-such-rule"); err == nil {
		t.Error("an unknown rule id is not an error")
	}
	if _, err := tenets.BuiltinPreset("no-such-preset"); err == nil {
		t.Error("an unknown preset name is not an error")
	}
}
