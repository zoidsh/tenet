package importer

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
)

// AgentSkill is what an agent is told about running the lint and acting on a
// finding. The plugin's skills/tenet/SKILL.md is this same text under a
// frontmatter block, and a test holds the two together.
//
//go:embed agent_skill.md
var AgentSkill string

// AgentHeading is the heading the instructions keep inside a file that holds
// other instructions too, so that a second run replaces the section the first
// one wrote rather than appending it again.
const AgentHeading = "## tenet"

// AgentTarget is one agent's way of reading instructions: a file of its own
// for Cursor, a section of the file it already reads for the rest.
type AgentTarget struct {
	Name string
	Path string

	// Content is the file to write, given what is there now, and whether it
	// replaces a section an earlier run wrote.
	Content func(existing []byte) (data []byte, replaced bool)
}

var agentTargets = []AgentTarget{
	{Name: "cursor", Path: ".cursor/rules/tenet.mdc", Content: cursorRule},
	{Name: "agents", Path: "AGENTS.md", Content: upsertSection},
	{Name: "claude", Path: "CLAUDE.md", Content: upsertSection},
}

// Agent is the target a --agent value names.
func Agent(name string) (AgentTarget, error) {
	for _, target := range agentTargets {
		if target.Name == name {
			return target, nil
		}
	}
	var names []string
	for _, target := range agentTargets {
		names = append(names, target.Name)
	}
	return AgentTarget{}, fmt.Errorf("unknown agent %q; known agents are %s", name, strings.Join(names, ", "))
}

// alwaysApply is what puts a Cursor rule in front of the model on every
// request, rather than when Cursor judges its description relevant.
const cursorFrontMatter = "---\ndescription: Run the tenet lint and act on what it finds\nalwaysApply: true\n---\n\n"

func cursorRule(existing []byte) ([]byte, bool) {
	return []byte(cursorFrontMatter + AgentSkill), len(existing) > 0
}

func upsertSection(existing []byte) ([]byte, bool) {
	section := AgentHeading + "\n\n" + AgentSkill
	start, end := findSection(existing)
	if start < 0 {
		if len(bytes.TrimSpace(existing)) == 0 {
			return []byte(section), false
		}
		var out bytes.Buffer
		out.Write(bytes.TrimRight(existing, "\n"))
		out.WriteString("\n\n" + section)
		return out.Bytes(), false
	}
	rest := existing[end:]
	if len(rest) > 0 {
		section += "\n"
	}
	out := append([]byte{}, existing[:start]...)
	out = append(out, section...)
	return append(out, rest...), true
}

// findSection is the byte range of the section, from its heading to the next
// heading that closes it, or -1 when the file has no such section.
func findSection(data []byte) (start, end int) {
	start, offset := -1, 0
	for _, line := range strings.SplitAfter(string(data), "\n") {
		text := strings.TrimRight(line, "\r\n")
		switch {
		case start < 0 && text == AgentHeading:
			start = offset
		case start >= 0 && closesSection(text):
			return start, offset
		}
		offset += len(line)
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(data)
}

// closesSection is true of a heading at the section's own level or above; a
// deeper heading is part of the section.
func closesSection(line string) bool {
	return strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ")
}
