package source

import "strings"

// A language's comment markers, as text. Nothing here parses a string
// literal, because doing so would mean a lexer per language; a marker inside
// one therefore opens a comment as far as this is concerned. A directive is a
// deliberate token, so the cost of that is a directive honoured in a place it
// was not needed, never one that is silently missed.
type commentSyntax struct {
	line   []string
	blocks []blockMarkers
}

type blockMarkers struct{ open, close string }

var (
	cStyle    = commentSyntax{line: []string{"//"}, blocks: []blockMarkers{{"/*", "*/"}}}
	hashStyle = commentSyntax{line: []string{"#"}}
	htmlStyle = commentSyntax{blocks: []blockMarkers{{"<!--", "-->"}}}
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
	"css":        {blocks: []blockMarkers{{"/*", "*/"}}},
	"php":        {line: []string{"//", "#"}, blocks: []blockMarkers{{"/*", "*/"}}},
	"python":     {line: []string{"#"}, blocks: []blockMarkers{{`"""`, `"""`}}},
	"ruby":       {line: []string{"#"}, blocks: []blockMarkers{{"=begin", "=end"}}},
	"shell":      hashStyle,
	"yaml":       hashStyle,
	"sql":        {line: []string{"--"}, blocks: []blockMarkers{{"/*", "*/"}}},
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
				at := strings.Index(line[pos:], open.close)
				if at < 0 {
					spans = append(spans, span{start, len(line)})
					break
				}
				end := pos + at + len(open.close)
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
		if at := strings.Index(line[pos:], marker); at >= 0 && (best < 0 || pos+at < best) {
			best, block = pos+at, nil
		}
	}
	for i := range syn.blocks {
		markers := &syn.blocks[i]
		if at := strings.Index(line[pos:], markers.open); at >= 0 && (best < 0 || pos+at < best) {
			best, block = pos+at, markers
		}
	}
	return best, block
}

func inSpans(spans []span, col int) bool {
	for _, s := range spans {
		if col >= s.start && col < s.end {
			return true
		}
	}
	return false
}
