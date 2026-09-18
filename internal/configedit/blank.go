package configedit

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// keepBlankLines puts the blank lines between a config's entries back into the
// head comments they sit above. yaml.v3 carries comments through a decode and
// an encode but nothing else that is not data, so without this a sync that
// changes one sentence rewrites every paragraph break in the file.
func keepBlankLines(doc *yaml.Node, lines []string) {
	// spoken is how many of a run's blank lines something already writes.
	spoken := map[int]int{}
	// A comment at the top of the file hangs on the document, and the encoder
	// writes one blank line under it by itself. Counting that one again would
	// add a blank on every write and walk it down the file.
	if doc.HeadComment != "" {
		if first, count := blankRun(doc.Line, lines); count > 0 {
			spoken[first] = 1
		}
	}
	claimBlankLines(doc, lines, spoken)
}

// claimBlankLines walks outermost first, so that the blank line above a tenet
// is credited to the entry rather than to the first key inside it, which
// begins on the same line.
func claimBlankLines(node *yaml.Node, lines []string, spoken map[int]int) {
	if node == nil {
		return
	}
	claim(node, lines, spoken)
	for _, child := range node.Content {
		claimBlankLines(child, lines, spoken)
	}
}

func claim(node *yaml.Node, lines []string, spoken map[int]int) {
	first, count := blankRun(node.Line-headLines(node.HeadComment), lines)
	if count == 0 {
		return
	}
	want := count - spoken[first]
	spoken[first] = count
	if want <= 0 {
		return
	}
	node.HeadComment = strings.Repeat("\n", want) + node.HeadComment
}

// blankRun is the stretch of blank lines that ends just above the given line,
// as a first line and a count.
func blankRun(above int, lines []string) (first, count int) {
	end := above - 1
	for end-count >= 1 && strings.TrimSpace(lines[end-count-1]) == "" {
		count++
	}
	return end - count + 1, count
}

func headLines(comment string) int {
	if comment == "" {
		return 0
	}
	return strings.Count(comment, "\n") + 1
}

// blockOpener matches the indicator that opens a literal or folded scalar,
// with the explicit indent or chomping a writer may have put on it.
var blockOpener = regexp.MustCompile(`(^|\s)[|>][+-]?\d*$`)

// unindentBlanks takes the indentation off the lines that hold nothing else.
// The encoder hangs a blank head-comment line at the indentation of the entry
// below it, and a config full of trailing spaces is what this package exists
// to avoid. Inside a block scalar the spaces are content, so those lines are
// left as the encoder wrote them.
func unindentBlanks(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	content, opened := -1, false
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			// A blank line is content only while there is more block scalar
			// after it. The ones that close a block are the paragraph break
			// before whatever follows, which is what the encoder indented.
			if content < 0 || !reaches(lines[i+1:], content) {
				lines[i] = ""
			}
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if opened {
			content, opened = indent, false
			continue
		}
		if content >= 0 {
			if indent >= content {
				continue
			}
			content = -1
		}
		opened = blockOpener.MatchString(strings.TrimRight(line, " "))
	}
	return []byte(strings.Join(lines, "\n"))
}

// reaches reports whether the next line with anything on it is still indented
// far enough to be a block scalar's content. The opener says where a block
// starts but nothing says where it ends, so the text after a blank line is
// what decides which side of the end that blank falls.
func reaches(lines []string, content int) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return len(line)-len(strings.TrimLeft(line, " ")) >= content
	}
	return false
}
