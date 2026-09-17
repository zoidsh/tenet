package judge

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashContext is how many non-blank lines either side of the offending line
// go into a finding's hash.
const HashContext = 3

// FindingHash identifies a finding by what it is about rather than where it
// is: the tenet, the offending line, and the lines around it, each stripped
// of its indentation so that reformatting does not retire a baseline entry.
// The window's own text is deliberately left out. A line inserted above a
// finding lands in that finding's window, so a hash over the window would
// change on almost any edit to the file, which is the opposite of what a
// baseline is for, and the window a line falls in also depends on how the
// request budget happened to split the file.
//
// lines are the file's, and id the one-based line the finding is on.
func FindingHash(tenetHash string, lines []string, id int) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(tenetHash)
	write(trimIndent(lines[id-1]))
	write(strings.Join(neighbourhood(lines, id), "\n"))
	return hex.EncodeToString(h.Sum(nil))
}

// neighbourhood is the offending line with the nearest non-blank lines either
// side of it, which is what tells two identical lines in one file apart.
func neighbourhood(lines []string, id int) []string {
	out := make([]string, 0, 2*HashContext+1)
	for i := id - 2; i >= 0 && len(out) < HashContext; i-- {
		if line := trimIndent(lines[i]); line != "" {
			out = append([]string{line}, out...)
		}
	}
	before := len(out)
	out = append(out, trimIndent(lines[id-1]))
	for i := id; i < len(lines) && len(out) < before+1+HashContext; i++ {
		if line := trimIndent(lines[i]); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func trimIndent(line string) string { return strings.TrimLeft(line, " \t") }
