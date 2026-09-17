package source

import (
	"regexp"
	"strings"
)

// A directive is a token in the code, not prose about the code: the model
// never sees it, so an ordinary comment saying a rule does not apply here
// cannot exempt the line.
var directivePattern = regexp.MustCompile(`\btenet:(ignore-next-line|ignore-file|ignore)\b(\s+[a-z0-9-]+(?:,[a-z0-9-]+)*)?`)

// AllTenets is the key under which a directive that names no tenet is
// recorded.
const AllTenets = ""

// Suppressions records which tenets a file or a line is exempt from.
type Suppressions struct {
	file  map[string]bool
	lines map[int]map[string]bool
}

func newSuppressions() *Suppressions {
	return &Suppressions{file: map[string]bool{}, lines: map[int]map[string]bool{}}
}

// File reports whether the whole file is exempt from a tenet.
func (s *Suppressions) File(tenet string) bool {
	if s == nil {
		return false
	}
	return s.file[AllTenets] || s.file[tenet]
}

// Line reports whether one line is exempt from a tenet.
func (s *Suppressions) Line(line int, tenet string) bool {
	if s == nil {
		return false
	}
	if s.File(tenet) {
		return true
	}
	at := s.lines[line]
	return at[AllTenets] || at[tenet]
}

func (s *Suppressions) addFile(ids []string) {
	for _, id := range ids {
		s.file[id] = true
	}
}

func (s *Suppressions) addLine(line int, ids []string) {
	at := s.lines[line]
	if at == nil {
		at = map[string]bool{}
		s.lines[line] = at
	}
	for _, id := range ids {
		at[id] = true
	}
}

// stripDirectives removes every directive token from the lines it is given and
// returns what each one suppresses. Line numbers survive because a directive
// is cut out of its line rather than the line out of the file.
func stripDirectives(lines []string) ([]string, *Suppressions) {
	sup := newSuppressions()
	out := make([]string, len(lines))
	copy(out, lines)
	for i, line := range lines {
		matches := directivePattern.FindAllStringSubmatchIndex(line, -1)
		if matches == nil {
			continue
		}
		for _, m := range matches {
			kind := line[m[2]:m[3]]
			ids := parseIDs(group(line, m, 2))
			switch kind {
			case "ignore":
				sup.addLine(i+1, ids)
			case "ignore-next-line":
				sup.addLine(i+2, ids)
			case "ignore-file":
				sup.addFile(ids)
			}
		}
		out[i] = cut(line, matches)
	}
	return out, sup
}

func group(line string, m []int, n int) string {
	start, end := m[2*n], m[2*n+1]
	if start < 0 {
		return ""
	}
	return line[start:end]
}

func parseIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{AllTenets}
	}
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []string{AllTenets}
	}
	return ids
}

func cut(line string, matches [][]int) string {
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(line[last:m[0]])
		last = m[1]
	}
	b.WriteString(line[last:])
	return b.String()
}
