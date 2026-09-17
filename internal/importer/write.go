package importer

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SlugWords is how many significant words of a tenet make its id: enough to
// tell two rules about comments apart, short enough to type in a directive.
const SlugWords = 5

// Header is the one thing a drafted file says that no tenet says, because a
// draft without criteria is the half of the format a reader has to be told
// about.
const Header = "# Criteria under a tenet, a true and a false description of what a violation looks like, sharpen its verdicts; see the Criteria section of the README.\n"

// starterTenet is the rule init writes when it finds no rule files at all:
// the comment rule this repository judges itself by, as somewhere to start.
const starterTenet = `  - id: comment-why
    tenet: A comment says why the code exists or why it is written this way, not what the code does.
    criteria:
      true: A comment inside a function body, or a doc comment that only repeats the identifier's name in other words, that restates what the code visibly does, narrates steps, names the operation, or is a section label. A reader of the code learns nothing new from it.
      false: The comment gives a reason, a constraint, a contract, a workaround for an external system, a reference explaining the choice, or a warning about ordering or invariants that the code does not show, or is a doc comment on an exported identifier stating what callers can rely on.
    severity: warn
    include: ["**/*.go"]
`

// StarterFile is the whole starter tenets.yml.
func StarterFile() []byte {
	return []byte(Header + "version: 1\ntenets:\n" + starterTenet)
}

var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "any": true, "are": true, "as": true,
	"at": true, "be": true, "been": true, "by": true, "for": true, "from": true,
	"has": true, "have": true, "in": true, "into": true, "is": true, "it": true,
	"its": true, "of": true, "on": true, "or": true, "our": true, "that": true,
	"the": true, "their": true, "them": true, "then": true, "there": true,
	"they": true, "this": true, "to": true, "was": true, "were": true,
	"what": true, "which": true, "will": true, "with": true, "you": true,
	"your": true,
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug names a tenet after its first significant words. Ids already taken are
// given back with a number, because two rules from the same section often open
// with the same words.
func Slug(text string, taken map[string]bool) string {
	var words []string
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = notSlug.ReplaceAllString(word, "")
		if word == "" || stopWords[word] {
			continue
		}
		words = append(words, word)
		if len(words) == SlugWords {
			break
		}
	}
	slug := strings.Join(words, "-")
	if slug == "" {
		slug = "tenet"
	}
	id := slug
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", slug, n)
	}
	taken[id] = true
	return id
}

// Accepted are the candidates that became tenets, in the order their files and
// lines were read in.
func Accepted(sorted []Sorted) []Sorted {
	var out []Sorted
	for _, c := range sorted {
		if c.Accepted {
			out = append(out, c)
		}
	}
	return out
}

// Assign names every accepted candidate. The report prints the ids beside the
// sentences they came from, so they are settled before the file is drafted and
// running it twice leaves them as they were.
func Assign(sorted []Sorted) {
	taken := map[string]bool{}
	for _, c := range sorted {
		if c.ID != "" {
			taken[c.ID] = true
		}
	}
	for i, c := range sorted {
		if c.Accepted && c.ID == "" {
			sorted[i].ID = Slug(c.Text, taken)
		}
	}
}

type draftFile struct {
	Version int          `yaml:"version"`
	Tenets  []draftTenet `yaml:"tenets"`
}

type draftTenet struct {
	ID       string `yaml:"id"`
	Tenet    string `yaml:"tenet"`
	Source   string `yaml:"source"`
	Severity string `yaml:"severity"`
}

// Draft is the tenets.yml for everything the sort accepted, which Assign has
// named by the time it is called.
func Draft(sorted []Sorted) ([]byte, error) {
	file := draftFile{Version: 1}
	for _, c := range Accepted(sorted) {
		file.Tenets = append(file.Tenets, draftTenet{
			ID:       c.ID,
			Tenet:    c.Text,
			Source:   fmt.Sprintf("%s:%d", c.File, c.Line),
			Severity: "warn",
		})
	}

	var body bytes.Buffer
	encoder := yaml.NewEncoder(&body)
	encoder.SetIndent(2)
	if err := encoder.Encode(file); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return append([]byte(Header), body.Bytes()...), nil
}
