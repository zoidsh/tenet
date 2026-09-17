package source

import (
	"fmt"
	"os"
)

// PRTextPath is the name a pull request's title and description are linted
// under. They have no path in the repository, and a finding still has to name
// a file the include globs can be written against.
const PRTextPath = "PULL_REQUEST"

func collectPRText(dir, path string, known map[string]bool) (*Set, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pull request text %s: %w", path, err)
	}
	// Root is where the lint was started, so that the text is reported under
	// the name the tenets match it by rather than a path on disk that only the
	// runner ever had.
	set := &Set{Root: dir}
	if reason := skipByContent(content); reason != "" {
		set.Skipped = append(set.Skipped, Skip{File: PRTextPath, Reason: reason})
		return set, nil
	}
	lines := splitLines(content)
	if len(lines) == 0 {
		return set, nil
	}
	file, err := newFile(PRTextPath, lines, nil, known)
	if err != nil {
		return nil, err
	}
	set.Files = append(set.Files, file)
	return set, nil
}
