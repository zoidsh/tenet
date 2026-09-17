package source

import (
	"fmt"
	"regexp"
	"strings"
)

// A directive is a token in the code, not prose about the code: the model
// never sees it, so an ordinary comment saying a rule does not apply here
// cannot exempt the line. The id list is greedy on purpose, so that prose
// after a directive is caught as an unknown tenet rather than silently
// suppressing one.
//
// Where the token counts is what keeps a file that merely talks about
// directives from being ruled by them: in code it has to sit in a comment, in
// prose and data at the start of a line or in an HTML comment. Only a commit
// message, which has no comment syntax of its own, still takes one anywhere.
var directivePattern = regexp.MustCompile(`\btenet:(ignore[a-z-]*)\b((?:[ \t]+[a-z0-9-]+(?:[ \t]*,[ \t]*[a-z0-9-]+)*)?)`)

// What may precede a directive at the start of a line in prose or data: list
// and quote markers, and a line comment marker, so that a directive can be
// written the way the format's own comments are.
var linePrefixPattern = regexp.MustCompile(`^[ \t]*(?:(?:[-*+>]|[0-9]+[.)])[ \t]+)*(?:(?:#+|//|--|;)[ \t]*)?$`)

// A fence around a code block in prose. The marker is not matched against the
// one that opened the block, because a mention inside a block whose fences do
// not pair up is still an example rather than a directive.
var fencePattern = regexp.MustCompile("^[ \t]*(?:```|~~~)")

// AllTenets is the key under which a directive that names no tenet is
// recorded.
const AllTenets = ""

// The directive keywords, and what each one exempts.
const (
	ignoreLine     = "ignore"
	ignoreNextLine = "ignore-next-line"
	ignoreFile     = "ignore-file"
)

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
// is cut out of its line rather than the line out of the file. A directive
// that names a tenet the config does not define, or a keyword that is not a
// directive, is an error: a typo that silently suppressed nothing would be
// worse than a failed run. A mention that does not count where it stands is
// not a directive at all: it stays in the text, and is never validated.
func stripDirectives(path string, lines []string, known map[string]bool) ([]string, *Suppressions, error) {
	sup := newSuppressions()
	out := make([]string, len(lines))
	copy(out, lines)
	counts := directiveCounts(path, lines)
	for i, line := range lines {
		matches := directivePattern.FindAllStringSubmatchIndex(line, -1)
		if matches == nil {
			continue
		}
		var kept [][]int
		for _, m := range matches {
			if !counts(i, m[0]) {
				continue
			}
			kept = append(kept, m)
			keyword := line[m[2]:m[3]]
			ids := parseIDs(group(line, m, 2))
			if err := check(path, i+1, keyword, ids, known); err != nil {
				return nil, nil, err
			}
			switch keyword {
			case ignoreLine:
				sup.addLine(i+1, ids)
			case ignoreNextLine:
				sup.addLine(i+2, ids)
			case ignoreFile:
				sup.addFile(ids)
			}
		}
		if kept != nil {
			out[i] = cut(line, kept)
		}
	}
	return out, sup, nil
}

// directiveCounts reports, for a zero-based line and the byte offset a
// directive was found at, whether that mention is a directive at all. The
// spans are scanned on the first mention rather than up front, because most
// files hold none and the scan walks the whole file.
func directiveCounts(path string, lines []string) func(line, col int) bool {
	anywhere := func(int, int) bool { return true }
	var syn commentSyntax
	prose, fences := false, false
	switch Kind(path) {
	case KindCommit, KindPR:
		return anywhere
	case KindProse:
		syn, prose, fences = htmlStyle, true, true
	case KindData:
		syn, prose = htmlStyle, true
	default:
		known := false
		if syn, known = commentSyntaxes[LanguageForPath(path)]; !known {
			return anywhere
		}
	}
	var spans [][]span
	var fenced []bool
	return func(line, col int) bool {
		if fences {
			if fenced == nil {
				fenced = fencedLines(lines)
			}
			if fenced[line] {
				return false
			}
		}
		if prose && linePrefixPattern.MatchString(lines[line][:col]) {
			return true
		}
		if spans == nil {
			spans = commentSpans(lines, syn)
		}
		return inSpans(spans[line], col)
	}
}

// fencedLines marks the lines of a prose file that a code block holds, the
// fences themselves included. A fence that is never closed runs to the end of
// the file, which is how the rest of the file reads to anyone looking at it.
func fencedLines(lines []string) []bool {
	out := make([]bool, len(lines))
	in := false
	for i, line := range lines {
		if fencePattern.MatchString(line) {
			out[i], in = true, !in
			continue
		}
		out[i] = in
	}
	return out
}

func check(path string, line int, keyword string, ids []string, known map[string]bool) error {
	switch keyword {
	case ignoreLine, ignoreNextLine, ignoreFile:
	default:
		return fmt.Errorf("%s:%d: unknown directive tenet:%s", path, line, keyword)
	}
	for _, id := range ids {
		if id != AllTenets && !known[id] {
			return fmt.Errorf("%s:%d: tenet:%s names %q, which is not a tenet in the config", path, line, keyword, id)
		}
	}
	return nil
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
