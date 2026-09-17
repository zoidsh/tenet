package jev

import (
	"context"
	"testing"
	"time"
)

// TestLive spends real money against the real API, so it only runs when a key
// is deliberately put in the environment.
func TestLive(t *testing.T) {
	key := KeyFromEnv()
	if key == "" {
		t.Skipf("set %s to run the live smoke test", APIKeyEnv)
	}
	client := New(key)

	t.Run("noul", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := client.Ask(ctx, "x := 1 // set x to 1", map[string]Question{
			"narrating": Noul("The comment says what the code does rather than why.", "", ""),
		})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if p := resp.Answers["narrating"].Prob(); p <= 0.8 {
			t.Errorf("probability = %v, want above 0.8", p)
		}
	})

	t.Run("choice", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// The narrating comment in the state below is the fixture this test
		// expects jev to catch, so it has to stay as it is written.
		state := "L001 func total(items []int) int {\nL002 \tsum := 0 // set sum to zero\n" // tenet:ignore comment-why
		where, err := Choice("Which line holds a comment that says what the code does rather than why?", map[string]any{
			"L001": "the function signature line",
			"L002": "the line declaring sum",
			"none": "no line holds such a comment",
		})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Ask(ctx, state, map[string]Question{"where": where})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		label, p := resp.Answers["where"].Top()
		if label != "L002" {
			t.Errorf("top = %q at %v, want L002", label, p)
		}
	})
}
