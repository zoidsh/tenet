package importer_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenet/internal/importer"
)

func syncReport() importer.SyncReport {
	added := candidate("CLAUDE.md", 7, mockSentence, true)
	added.ID = "never-mock-anything-tests"
	return importer.SyncReport{
		Plan: importer.Plan{
			Added: []importer.Sorted{added},
			Changed: []importer.Change{
				{
					Entry:       tenetEntry("comment-says-why", commentSentence, importer.SourceLine("CLAUDE.md", commentSentence)),
					Old:         commentSentence,
					New:         reworded,
					File:        "CLAUDE.md",
					TextUpdated: true,
				},
				{
					Entry: importer.Entry{Rule: "no-fallback", Source: importer.SourceLine("AGENTS.md", testsSentence)},
					Old:   testsSentence,
					New:   "Run every test before you call a branch done.",
					File:  "AGENTS.md",
				},
			},
			Restated: []importer.Restated{
				{
					Entry:  tenetEntry("drafted-long-ago", mockSentence, "CLAUDE.md:12"),
					Source: importer.SourceLine("CLAUDE.md", mockSentence),
				},
			},
			Stale: []importer.Entry{
				tenetEntry("gone-away", "Nothing says this any more.", importer.SourceLine("CLAUDE.md", "Nothing says this any more.")),
			},
		},
		Stats: importer.Stats{
			Candidates: 40, Calls: 3, CacheHits: 37, CostUSD: 0.0021, Duration: 700 * time.Millisecond,
		},
		Written: ".tenet/config.yml",
	}
}

func TestSyncReportText(t *testing.T) {
	var b bytes.Buffer
	if err := syncReport().Text(&b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"added",
		"never-mock-anything-tests",
		"CLAUDE.md: " + mockSentence,
		"changed",
		"comment-says-why",
		"re-run tenet check comment-says-why",
		"rules no-fallback",
		"restated",
		"drafted-long-ago",
		"the source named a line, which now names the sentence",
		"stale",
		"gone-away",
		"1 added, 2 changed, 1 restated, 1 stale, written to .tenet/config.yml · 40 sentences, 3 calls, 37 cached",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not hold %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "the tenet sentence stays as you wrote it") {
		t.Errorf("the tenet did move with its sentence:\n%s", got)
	}
}

// A tenet the rewording did not reach has to say so, because a person reading
// the report is the only one who can settle it.
func TestSyncReportSaysWhenATenetStayedAsWritten(t *testing.T) {
	r := syncReport()
	r.Plan.Changed[0].TextUpdated = false
	var b bytes.Buffer
	if err := r.Text(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "the tenet sentence stays as you wrote it") {
		t.Errorf("the report is:\n%s", b.String())
	}
}

func TestSyncReportSummaryWithoutAWrite(t *testing.T) {
	r := syncReport()
	r.Written, r.DryRun = "", true
	if !strings.Contains(r.Summary(), "nothing written (--dry-run)") {
		t.Errorf("summary is %q", r.Summary())
	}
	r.DryRun = false
	if !strings.Contains(r.Summary(), "nothing to write") {
		t.Errorf("summary is %q", r.Summary())
	}
}

func TestSyncReportJSON(t *testing.T) {
	var b bytes.Buffer
	if err := syncReport().JSON(&b); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Added []struct {
			Tenet  string   `json:"tenet"`
			Source string   `json:"source"`
			Kind   []string `json:"kind"`
		} `json:"added"`
		Changed []struct {
			Tenet       string `json:"tenet"`
			Rule        string `json:"rule"`
			Old         string `json:"old"`
			New         string `json:"new"`
			TextUpdated bool   `json:"text_updated"`
		} `json:"changed"`
		Restated []struct {
			Tenet  string `json:"tenet"`
			Rule   string `json:"rule"`
			Source string `json:"source"`
		} `json:"restated"`
		Stale []struct {
			Tenet  string `json:"tenet"`
			Rule   string `json:"rule"`
			Source string `json:"source"`
		} `json:"stale"`
		Candidates []any   `json:"candidates"`
		Written    *string `json:"written"`
		DryRun     bool    `json:"dry_run"`
		Stats      struct {
			Candidates int `json:"candidates"`
			Calls      int `json:"calls"`
			CacheHits  int `json:"cache_hits"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, b.String())
	}
	if len(got.Added) != 1 || got.Added[0].Tenet != "never-mock-anything-tests" {
		t.Fatalf("added %#v", got.Added)
	}
	if got.Added[0].Source != importer.SourceLine("CLAUDE.md", mockSentence) {
		t.Errorf("source is %q", got.Added[0].Source)
	}
	if len(got.Added[0].Kind) != 1 || got.Added[0].Kind[0] != "code" {
		t.Errorf("kind is %#v", got.Added[0].Kind)
	}
	if len(got.Changed) != 2 || got.Changed[0].Tenet != "comment-says-why" || !got.Changed[0].TextUpdated {
		t.Fatalf("changed %#v", got.Changed)
	}
	if got.Changed[0].Old != commentSentence || got.Changed[0].New != reworded {
		t.Errorf("changed %#v", got.Changed[0])
	}
	if got.Changed[1].Rule != "no-fallback" || got.Changed[1].Tenet != "" {
		t.Errorf("a rules comment is reported as %#v", got.Changed[1])
	}
	if len(got.Restated) != 1 || got.Restated[0].Tenet != "drafted-long-ago" {
		t.Fatalf("restated %#v", got.Restated)
	}
	if got.Restated[0].Source != importer.SourceLine("CLAUDE.md", mockSentence) {
		t.Errorf("restated source is %q", got.Restated[0].Source)
	}
	if len(got.Stale) != 1 || got.Stale[0].Tenet != "gone-away" {
		t.Errorf("stale %#v", got.Stale)
	}
	if got.Written == nil || *got.Written != ".tenet/config.yml" || got.DryRun {
		t.Errorf("written %v, dry run %v", got.Written, got.DryRun)
	}
	if got.Stats.Candidates != 40 || got.Stats.Calls != 3 || got.Stats.CacheHits != 37 {
		t.Errorf("stats %#v", got.Stats)
	}
}

// The arrays are always arrays, because an agent reading the report should not
// have to tell an empty one from a null.
func TestSyncReportJSONHoldsEmptyArrays(t *testing.T) {
	var b bytes.Buffer
	if err := (importer.SyncReport{}).JSON(&b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"added": []`, `"changed": []`, `"restated": []`, `"stale": []`, `"candidates": []`, `"written": null`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the report does not hold %s:\n%s", want, b.String())
		}
	}
}
