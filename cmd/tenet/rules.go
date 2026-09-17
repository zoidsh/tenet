package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/tenets"
)

// tenetWidth is how much of a tenet a listing shows before it is cut, so that
// a row stays on one terminal line beside its tags and presets.
const tenetWidth = 56

func newRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rules [rule-id]",
		Short: "List the rules that ship in the binary, or print one of them",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runRule(cmd, args[0])
			}
			return runRules(cmd)
		},
	}
}

func runRules(cmd *cobra.Command) error {
	rules, err := tenets.BuiltinRules()
	if err != nil {
		return fail(err)
	}
	var b strings.Builder
	table := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprint(table, "id\ttags\tpresets\ttenet\n")
	for _, rule := range rules {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n",
			rule.ID, list(rule.Tenet.Tags), list(rule.Presets), cut(rule.Tenet.Tenet))
	}
	if err := table.Flush(); err != nil {
		return fail(err)
	}
	_, err = io.WriteString(cmd.OutOrStdout(), b.String())
	return err
}

func runRule(cmd *cobra.Command, id string) error {
	rule, err := tenets.BuiltinRule(id)
	if err != nil {
		return fail(err)
	}
	var b strings.Builder
	b.WriteString("rules/" + rule.ID + "/rule.yml\n\n")
	b.WriteString(rule.YAML)
	if rule.Doc != "" {
		b.WriteString("\n" + rule.Doc + "\n")
	}
	b.WriteString("\n" + exampleCount(rule.Tenet.Examples) + "\n")
	_, err = io.WriteString(cmd.OutOrStdout(), b.String())
	return err
}

func newPresetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "presets",
		Short: "List the presets that ship in the binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			presets, err := tenets.BuiltinPresets()
			if err != nil {
				return fail(err)
			}
			var b strings.Builder
			for i, p := range presets {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(p.Name + "  " + p.Description + "\n")
				b.WriteString("  " + list(p.Rules) + "\n")
			}
			_, err = io.WriteString(cmd.OutOrStdout(), b.String())
			return err
		},
	}
}

func exampleCount(examples []tenets.Example) string {
	var violations, oks int
	for _, e := range examples {
		if e.Label == tenets.LabelViolation {
			violations++
			continue
		}
		oks++
	}
	return fmt.Sprintf("%d examples (%d violation, %d ok)", len(examples), violations, oks)
}

func list(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

func cut(text string) string {
	runes := []rune(text)
	if len(runes) <= tenetWidth {
		return text
	}
	return string(runes[:tenetWidth-1]) + "…"
}
