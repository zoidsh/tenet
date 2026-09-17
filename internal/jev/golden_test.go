package jev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures are response bodies recorded from the live API during the
// design spike, so they guard the decoder against wire details no handwritten
// sample would think to include. The live API omitted the documented "model"
// field in every one of them, so these tests assert nothing about it; do not
// "fix" the fixtures by adding it.
func TestGoldenResponses(t *testing.T) {
	for _, tc := range []struct {
		file      string
		answers   int
		noul      map[string]float64
		topLabel  string
		topProb   float64
		topAnswer string
		inTokens  int
	}{
		{
			file:     "noul_single.json",
			answers:  1,
			noul:     map[string]float64{"a": 0.92},
			inTokens: 428,
		},
		{
			file:     "noul_criteria.json",
			answers:  1,
			noul:     map[string]float64{"c": 0.06},
			inTokens: 663,
		},
		{
			file:      "fanout_noul_choice.json",
			answers:   5,
			noul:      map[string]float64{"a": 0.92, "b": 0.96, "c": 0.96, "d": 0.05},
			topAnswer: "where",
			topLabel:  "L007",
			topProb:   0.61,
			inTokens:  1071,
		},
		{
			file:      "fanout_negative.json",
			answers:   5,
			noul:      map[string]float64{"a": 0.25, "b": 0.12, "c": 0.11, "d": 0.84},
			topAnswer: "where",
			topLabel:  "none",
			topProb:   0.47,
			inTokens:  1204,
		},
		{
			file:      "choice_location.json",
			answers:   1,
			topAnswer: "where@as_specified_alone",
			topLabel:  "none",
			topProb:   0.49,
			inTokens:  660,
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			client, _ := testClient(t, srv)
			resp, err := client.Ask(context.Background(), "recorded state", map[string]Question{
				"a": Noul("recorded question", "", ""),
			})
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if len(resp.Answers) != tc.answers {
				t.Fatalf("answers = %d, want %d", len(resp.Answers), tc.answers)
			}
			for name, want := range tc.noul {
				answer := resp.Answers[name]
				if answer.Type != KindNoul {
					t.Errorf("%s type = %q", name, answer.Type)
				}
				if answer.Prob() != want {
					t.Errorf("%s prob = %v, want %v", name, answer.Prob(), want)
				}
			}
			if tc.topAnswer != "" {
				answer := resp.Answers[tc.topAnswer]
				if answer.Type != KindChoice {
					t.Errorf("%s type = %q", tc.topAnswer, answer.Type)
				}
				label, p := answer.Top()
				if label != tc.topLabel || p != tc.topProb {
					t.Errorf("%s top = %q %v, want %q %v", tc.topAnswer, label, p, tc.topLabel, tc.topProb)
				}
			}
			if resp.Usage.InputTokens != tc.inTokens {
				t.Errorf("input tokens = %d, want %d", resp.Usage.InputTokens, tc.inTokens)
			}
		})
	}
}

// The recorded choice where the model's own pick disagrees with the top of the
// distribution is exactly why Top reads the probabilities.
func TestGoldenTopIgnoresConfidence(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "fanout_negative.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client, _ := testClient(t, srv)
	resp, err := client.Ask(context.Background(), "recorded state", map[string]Question{
		"a": Noul("recorded question", "", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := resp.Answers["where"]
	if _, p := answer.Top(); p == answer.Confidence {
		t.Errorf("top probability %v equals confidence %v; the fixture no longer distinguishes them", p, answer.Confidence)
	}
}
