package source

import (
	"fmt"
	"os"
	"strings"
)

// CommitMsgPath is the name a commit message is linted under. The message has
// no path in the repository, and a finding still has to name a file the
// include globs can be written against; it is the name git itself keeps the
// message under.
const CommitMsgPath = "COMMIT_EDITMSG"

// scissorsLine is what `git commit --verbose` puts above the diff it appends
// to the message. Git drops that line and everything under it, so judging any
// of it would judge text that is never committed.
const scissorsLine = "# ------------------------ >8 ------------------------"

func collectCommitMsg(dir, path string, known map[string]bool) (*Set, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("commit message %s: %w", path, err)
	}
	// Root is where the lint was started, so that the message is reported
	// under the name the tenets match it by rather than a path on disk that
	// nobody can open once the commit is over.
	set := &Set{Root: dir}
	if reason := skipByContent(content); reason != "" {
		set.Skipped = append(set.Skipped, Skip{File: CommitMsgPath, Reason: reason})
		return set, nil
	}
	lines := stripGitComments(splitLines(content))
	if len(lines) == 0 {
		return set, nil
	}
	file, err := newFile(CommitMsgPath, lines, nil, known)
	if err != nil {
		return nil, err
	}
	set.Files = append(set.Files, file)
	return set, nil
}

// stripGitComments removes what git itself strips before it stores the
// message. A comment is blanked rather than cut out so that a finding names
// the line the editor showed the message on.
func stripGitComments(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == scissorsLine {
			out = out[:i]
			break
		}
		if strings.HasPrefix(line, "#") {
			out[i] = ""
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}
