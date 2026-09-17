package source

import "strings"

// A language's comment markers, as text. Nothing here parses a string
// literal, because doing so would mean a lexer per language; a marker inside
// one therefore opens or closes a comment as far as this is concerned. The
// approximation usually errs towards honouring a directive that needed no
// comment to be in, but it can miss one: a `//` inside a string swallows a
// `/*` later on the same line, so the block it opened is never seen and a
// directive on the lines below it goes unread.
type commentSyntax struct {
	line   []string
	blocks []blockMarkers
}

// anchored marks a block whose markers only count at the start of a line,
// which is what Ruby requires of =begin and =end.
type blockMarkers struct {
	open, close string
	anchored    bool
}

var (
	cStyle    = commentSyntax{line: []string{"//"}, blocks: []blockMarkers{{open: "/*", close: "*/"}}}
	hashStyle = commentSyntax{line: []string{"#"}}
	htmlStyle = commentSyntax{blocks: []blockMarkers{{open: "<!--", close: "-->"}}}
)

// commentSyntaxes is keyed by the language names LanguageForPath returns. A
// language that is missing has no entry rather than an empty one, so that an
// unknown language falls back to matching a directive anywhere on the line.
var commentSyntaxes = map[string]commentSyntax{
	"go":         cStyle,
	"typescript": cStyle,
	"javascript": cStyle,
	"rust":       cStyle,
	"java":       cStyle,
	"kotlin":     cStyle,
	"swift":      cStyle,
	"c":          cStyle,
	"cpp":        cStyle,
	"csharp":     cStyle,
	"css":        {blocks: []blockMarkers{{open: "/*", close: "*/"}}},
	"php":        {line: []string{"//", "#"}, blocks: []blockMarkers{{open: "/*", close: "*/"}}},
	"python":     {line: []string{"#"}, blocks: []blockMarkers{{open: `"""`, close: `"""`}}},
	"ruby":       {line: []string{"#"}, blocks: []blockMarkers{{open: "=begin", close: "=end", anchored: true}}},
	"shell":      hashStyle,
	"yaml":       hashStyle,
	"sql":        {line: []string{"--"}, blocks: []blockMarkers{{open: "/*", close: "*/"}}},
	"html":       htmlStyle,
	"markdown":   htmlStyle,
}

// span is a half-open byte range within one line.
type span struct{ start, end int }

// commentSpans marks the comment text on each line. A block comment carries
// its state from line to line, which is why the whole file is scanned at once
// rather than each line on its own.
func commentSpans(lines []string, syn commentSyntax) [][]span {
	out := make([][]span, len(lines))
	var open *blockMarkers
	for i, line := range lines {
		var spans []span
		pos, start := 0, 0
		for {
			if open != nil {
				at := indexMarker(line, pos, open.close, open.anchored)
				if at < 0 {
					spans = append(spans, span{start, len(line)})
					break
				}
				end := at + len(open.close)
				spans = append(spans, span{start, end})
				pos, open = end, nil
				continue
			}
			at, block := earliestMarker(line, pos, syn)
			if at < 0 {
				break
			}
			if block == nil {
				spans = append(spans, span{at, len(line)})
				break
			}
			open, start, pos = block, at, at+len(block.open)
		}
		out[i] = spans
	}
	return out
}

// earliestMarker finds the first comment marker at or after pos, returning the
// block it opens or nil for a line marker.
func earliestMarker(line string, pos int, syn commentSyntax) (int, *blockMarkers) {
	best := -1
	var block *blockMarkers
	for _, marker := range syn.line {
		if at := indexMarker(line, pos, marker, false); at >= 0 && (best < 0 || at < best) {
			best, block = at, nil
		}
	}
	for i := range syn.blocks {
		markers := &syn.blocks[i]
		if at := indexMarker(line, pos, markers.open, markers.anchored); at >= 0 && (best < 0 || at < best) {
			best, block = at, markers
		}
	}
	return best, block
}

// indexMarker is where a marker stands at or after pos, as an index into the
// whole line, or -1. An anchored marker counts only at the start of the line.
func indexMarker(line string, pos int, marker string, anchored bool) int {
	if anchored {
		if pos == 0 && strings.HasPrefix(line, marker) {
			return 0
		}
		return -1
	}
	at := strings.Index(line[pos:], marker)
	if at < 0 {
		return -1
	}
	return pos + at
}

func inSpans(spans []span, col int) bool {
	for _, s := range spans {
		if col >= s.start && col < s.end {
			return true
		}
	}
	return false
}
