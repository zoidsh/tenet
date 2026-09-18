package importer

import (
	"regexp"
	"strconv"
	"strings"
)

// SourceLine is what a tenet's source field says: the file the rule was read
// from and the sentence as that file wrote it. The sentence rather than a line
// number, because a line number goes stale the moment anything above it is
// edited, and sync has to know which sentence a tenet stood behind to tell a
// reworded rule from a deleted one.
func SourceLine(file, text string) string {
	return file + ": " + text
}

// lineForm matches the source init wrote before the sentence was carried in
// it. The digits follow the colon with no space between, so a sentence opening
// with a number is never read as a line.
var lineForm = regexp.MustCompile(`^(.+):(\d+)$`)

// namesAFile holds the greedy match to a path: without this, a sentence
// ending in something like 16:9 is read as line 9 of a file named after the
// whole sentence in front of it.
func namesAFile(file string) bool { return !strings.Contains(file, ": ") }

// Source is a source field taken apart.
type Source struct {
	File string

	// Sentence is what the rule file said, empty for the old file:line form.
	Sentence string

	// Line is the line the old form named, zero for the new form.
	Line int
}

// ParseSource reads a source field. Anything that is neither form, such as the
// prose provenance a built-in rule carries, comes back not ok.
func ParseSource(s string) (Source, bool) {
	s = strings.TrimSpace(s)
	if m := lineForm.FindStringSubmatch(s); m != nil && namesAFile(m[1]) {
		line, err := strconv.Atoi(m[2])
		if err != nil || line < 1 {
			return Source{}, false
		}
		return Source{File: m[1], Line: line}, true
	}
	file, sentence, ok := strings.Cut(s, ": ")
	file, sentence = strings.TrimSpace(file), strings.TrimSpace(sentence)
	if !ok || file == "" || sentence == "" {
		return Source{}, false
	}
	return Source{File: file, Sentence: sentence}, true
}
