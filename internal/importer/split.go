// Package importer drafts a tenets.yml from the rule files a coding agent
// already reads.
package importer

import (
	"regexp"
	"strings"
)

// Word counts a candidate has to fall between. A shorter line is a heading in
// disguise or a fragment; a longer one is a paragraph the model would have to
// judge as a whole.
const (
	MinWords = 4
	MaxWords = 60
)

// Candidate is one sentence or list item of a rule file, the unit the sort
// decides on.
type Candidate struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	fenceLine   = regexp.MustCompile("^\\s*(```|~~~)")
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	itemLine    = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$`)
	boldStars   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	boldScores  = regexp.MustCompile(`(^|\W)__([^_]+)__($|\W)`)
	codeSpan    = regexp.MustCompile("`[^`]*`")
	linkOnly    = regexp.MustCompile(`^<?\[[^\]]*\]\([^)]*\)>?$`)
)

// Split turns one rule file into the candidates the sort will be asked about.
func Split(file string, data []byte) []Candidate {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = blankComments(text)
	s := &splitter{file: file}
	s.run(strings.Split(text, "\n"))
	return s.out
}

// blankComments empties every HTML comment while keeping the newlines inside
// it, so that the line numbers of everything after it still hold.
func blankComments(text string) string {
	return htmlComment.ReplaceAllStringFunc(text, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	})
}

// piece is one source line of a paragraph, kept apart so that a sentence can
// be traced back to the line it starts on.
type piece struct {
	line int
	text string
}

type splitter struct {
	file     string
	headings []string
	out      []Candidate

	item *piece
	para []piece
}

func (s *splitter) run(lines []string) {
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if t := strings.TrimSpace(lines[i]); t == "---" || t == "..." {
				start = i + 1
				break
			}
		}
	}

	inFence := false
	for i := start; i < len(lines); i++ {
		line, number := lines[i], i+1
		if fenceLine.MatchString(line) {
			if !inFence {
				s.flush()
			}
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.TrimSpace(line) == "" {
			s.flush()
			continue
		}
		if m := headingLine.FindStringSubmatch(line); m != nil {
			s.flush()
			s.heading(len(m[1]), strings.TrimSpace(strings.TrimRight(m[2], " #")))
			continue
		}
		if m := itemLine.FindStringSubmatch(line); m != nil {
			s.flush()
			s.item = &piece{line: number, text: strings.TrimSpace(m[2])}
			continue
		}
		if s.item != nil && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			s.item.text += " " + strings.TrimSpace(line)
			continue
		}
		s.closeItem()
		s.para = append(s.para, piece{line: number, text: strings.TrimSpace(line)})
	}
	s.flush()
}

func (s *splitter) heading(level int, text string) {
	for len(s.headings) < level-1 {
		s.headings = append(s.headings, "")
	}
	s.headings = append(s.headings[:level-1], text)
}

func (s *splitter) chain() string {
	var parts []string
	for _, h := range s.headings {
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, " > ")
}

func (s *splitter) closeItem() {
	if s.item != nil {
		s.emit(s.item.line, s.item.text)
		s.item = nil
	}
}

func (s *splitter) flush() {
	s.closeItem()
	if len(s.para) > 0 {
		for _, sentence := range sentences(s.para) {
			s.emit(sentence.line, sentence.text)
		}
		s.para = nil
	}
}

func (s *splitter) emit(line int, text string) {
	text = clean(text)
	if !keep(text) {
		return
	}
	s.out = append(s.out, Candidate{File: s.file, Line: line, Heading: s.chain(), Text: text})
}

// clean strips the emphasis a rule file puts on part of a rule: the markers
// are markdown for the eye, and a tenet is read as a sentence.
func clean(text string) string {
	return strings.TrimSpace(outsideCode(strings.TrimSpace(text), stripEmphasis))
}

// codeMask stands in for a backticked span while the emphasis around it is
// stripped. It is a byte no markdown file holds, so nothing else can match it.
const codeMask = "\x00"

// outsideCode applies f to the text with every backticked span held out of its
// reach, because a code span means whatever the language it is written in says
// it means. The span is masked rather than skipped so that emphasis wrapped
// around one is still recognised as a pair.
func outsideCode(text string, f func(string) string) string {
	spans := codeSpan.FindAllString(text, -1)
	out := f(codeSpan.ReplaceAllLiteralString(text, codeMask))
	for _, span := range spans {
		out = strings.Replace(out, codeMask, span, 1)
	}
	return out
}

// stripEmphasis takes the markers off a bold span and leaves a mismatched pair
// alone. Asterisks are never part of a name, so every pair of them goes;
// underscores are, so a run of them is only read as emphasis when the sentence
// holds exactly one pair, which leaves Python names such as __slots__ and
// __repr__ as their authors wrote them.
func stripEmphasis(text string) string {
	text = boldStars.ReplaceAllString(text, "$1")
	if strings.Count(text, "__") == 2 {
		text = boldScores.ReplaceAllString(text, "$1$2$3")
	}
	return text
}

func keep(text string) bool {
	if isLinkOrPath(text) {
		return false
	}
	n := len(strings.Fields(text))
	return n >= MinWords && n <= MaxWords
}

// isLinkOrPath drops a line that only points somewhere: it names no rule, and
// a URL is full of the dots and slashes that would confuse the model.
func isLinkOrPath(text string) bool {
	if linkOnly.MatchString(text) {
		return true
	}
	if strings.ContainsAny(text, " \t") {
		return false
	}
	bare := strings.Trim(text, "<>`")
	return strings.Contains(bare, "://") || strings.Contains(bare, "/") ||
		strings.HasPrefix(bare, "~") || strings.HasPrefix(bare, ".")
}

// sentences splits a paragraph on sentence-ending punctuation, keeping the
// line each sentence began on.
func sentences(paragraph []piece) []piece {
	var b strings.Builder
	var lineAt []int
	for i, p := range paragraph {
		if i > 0 {
			b.WriteByte(' ')
			lineAt = append(lineAt, p.line)
		}
		b.WriteString(p.text)
		for range len(p.text) {
			lineAt = append(lineAt, p.line)
		}
	}
	text := b.String()

	var out []piece
	start := 0
	for _, end := range bounds(text) {
		if piece := strings.TrimSpace(text[start:end]); piece != "" {
			out = append(out, pieceAt(piece, lineAt, text, start))
		}
		start = end
	}
	if rest := strings.TrimSpace(text[start:]); rest != "" {
		out = append(out, pieceAt(rest, lineAt, text, start))
	}
	return out
}

func pieceAt(text string, lineAt []int, whole string, start int) piece {
	for start < len(whole) && whole[start] == ' ' {
		start++
	}
	return piece{line: lineAt[start], text: text}
}

// bounds are the offsets a paragraph breaks at: an end of sentence is
// punctuation that either ends the paragraph or is followed by a space and a
// capital, which leaves "e.g. this" and version numbers alone. Backticked
// spans are skipped whole, because `pkg.Func()` is code, not prose.
func bounds(text string) []int {
	var out []int
	inTick := false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '`':
			inTick = !inTick
		case inTick, c != '.' && c != '!' && c != '?':
		case strings.TrimSpace(text[i+1:]) == "":
			out = append(out, i+1)
		case text[i+1] == ' ':
			j := i + 1
			for j < len(text) && text[j] == ' ' {
				j++
			}
			if j < len(text) && text[j] >= 'A' && text[j] <= 'Z' {
				out = append(out, i+1)
			}
		}
	}
	return out
}
