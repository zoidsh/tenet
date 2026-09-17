// The recording sleeper and the fixed random source are how every retry test
// below asserts on backoff without waiting out the real delays: the seams are
// the subject of those tests, not a way around a real dependency.
// tenet:ignore-file no-mocking
package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordingSleeper stands in for time.Sleep so the retry tests run in
// microseconds and can assert on the delays that were asked for.
type recordingSleeper struct {
	delays []time.Duration
	before func()
}

func (s *recordingSleeper) sleep(ctx context.Context, d time.Duration) error {
	s.delays = append(s.delays, d)
	if s.before != nil {
		s.before()
	}
	return ctx.Err()
}

func testClient(t *testing.T, srv *httptest.Server, opts ...Option) (*Client, *recordingSleeper) {
	t.Helper()
	sleeper := &recordingSleeper{}
	base := []Option{
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithSleep(sleeper.sleep),
		WithRandom(func() float64 { return 0 }),
	}
	return New("test-key", append(base, opts...)...), sleeper
}

func TestAskDecodesEveryAnswerKind(t *testing.T) {
	const body = `{
	  "model": "jev-1.13.0",
	  "answers": {
	    "narrating": {"type": "noul", "noul": 0.91},
	    "where": {"type": "choice", "choice": "L002", "confidence": 0.8,
	              "probabilities": {"L001": 0.01, "L002": 0.82, "none": 0.17}},
	    "severity": {"type": "score", "score": 2, "confidence": 0.7,
	                 "legend": {"0": "fine", "1": "minor", "2": "bad"},
	                 "probabilities": {"0": 0.1, "1": 0.2, "2": 0.7}}
	  },
	  "usage": {"input_tokens": 1000000, "output_tokens": 12}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(requestIDHeader, "req_123")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	client, _ := testClient(t, srv)
	resp, err := client.Ask(context.Background(), "x := 1", map[string]Question{
		"narrating": Noul("The comment says what, not why.", "", ""),
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if resp.Model != "jev-1.13.0" {
		t.Errorf("model = %q", resp.Model)
	}
	if resp.RequestID != "req_123" {
		t.Errorf("request id = %q", resp.RequestID)
	}
	if got := resp.Answers["narrating"].Prob(); got != 0.91 {
		t.Errorf("noul prob = %v", got)
	}
	label, p := resp.Answers["where"].Top()
	if label != "L002" || p != 0.82 {
		t.Errorf("choice top = %q %v", label, p)
	}
	score := resp.Answers["severity"]
	if score.Score != 2 || score.Legend["2"] != "bad" || score.Probabilities["2"] != 0.7 {
		t.Errorf("score answer = %+v", score)
	}
	if resp.Usage.InputTokens != 1000000 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if got := Cost(resp.Usage); math.Abs(got-0.042) > 1e-9 {
		t.Errorf("Cost = %v, want 0.042", got)
	}
}

func TestAskSendsHeadersAndBody(t *testing.T) {
	type captured struct {
		auth, contentType, userAgent, path, method string
		body                                       request
	}
	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.contentType = r.Header.Get("Content-Type")
		got.userAgent = r.Header.Get("User-Agent")
		got.path = r.URL.Path
		got.method = r.Method
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_, _ = io.WriteString(w, `{"model":"jev-1.13.0","answers":{},"usage":{}}`)
	}))
	defer srv.Close()

	client, _ := testClient(t, srv, WithUserAgent("tenet/9.9.9"))
	if _, err := client.Ask(context.Background(), "state text", map[string]Question{
		"a": Noul("Is it so?", "yes", "no"),
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if got.method != http.MethodPost || got.path != "/v1/systemone" {
		t.Errorf("%s %s", got.method, got.path)
	}
	if got.auth != "Bearer test-key" {
		t.Errorf("authorization = %q", got.auth)
	}
	if got.contentType != "application/json" {
		t.Errorf("content-type = %q", got.contentType)
	}
	if got.userAgent != "tenet/9.9.9" {
		t.Errorf("user-agent = %q", got.userAgent)
	}
	if got.body.State != "state text" {
		t.Errorf("state = %q", got.body.State)
	}
	if got.body.Model != DefaultModel {
		t.Errorf("model = %q, want %q", got.body.Model, DefaultModel)
	}
	if got.body.Questions["a"].Type != KindNoul {
		t.Errorf("question = %+v", got.body.Questions["a"])
	}
}

func TestDefaultUserAgentCarriesVersion(t *testing.T) {
	client := New("k")
	if !strings.HasPrefix(client.opts.UserAgent, "tenet/") {
		t.Errorf("user agent = %q", client.opts.UserAgent)
	}
}

func TestConstructorsValidateCounts(t *testing.T) {
	if _, err := Choice("pick", map[string]any{}); err == nil {
		t.Error("empty choice accepted")
	}
	labels := make(map[string]any, MaxChoiceLabels+1)
	for i := 0; i <= MaxChoiceLabels; i++ {
		labels[string(rune('a'+i%26))+string(rune('a'+i/26))] = nil
	}
	if _, err := Choice("pick", labels); err == nil {
		t.Error("oversized choice accepted")
	}
	if _, err := Score("rate", []string{"only"}); err == nil {
		t.Error("one-level score accepted")
	}
	if _, err := Score("rate", make([]string, MaxScoreLevels+1)); err == nil {
		t.Error("eleven-level score accepted")
	}
	if _, err := Choice("pick", map[string]any{"a": "first", "b": nil}); err != nil {
		t.Errorf("valid choice rejected: %v", err)
	}
	if _, err := Score("rate", []string{"low", "high"}); err != nil {
		t.Errorf("valid score rejected: %v", err)
	}
}

func TestAskValidatesBeforeSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()
	client, _ := testClient(t, srv)

	for name, questions := range map[string]map[string]Question{
		"no questions":  {},
		"unknown kind":  {"a": {Type: "vibe"}},
		"empty choice":  {"a": {Type: KindChoice, Criteria: map[string]any{}}},
		"short score":   {"a": {Type: KindScore, Criteria: []string{"only"}}},
		"choice a list": {"a": {Type: KindChoice, Criteria: []string{"a"}}},
	} {
		if _, err := client.Ask(context.Background(), "s", questions); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("sent %d requests despite invalid questions", got)
	}
}

func TestErrorBodyShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{name: "error string", status: 400, body: `{"error": "bad state"}`, want: "bad state"},
		{name: "error object", status: 400, body: `{"error": {"message": "bad model"}}`, want: "bad model"},
		{name: "message", status: 401, body: `{"message": "invalid api key"}`, want: "invalid api key"},
		{name: "detail string", status: 403, body: `{"detail": "forbidden here"}`, want: "forbidden here"},
		{name: "detail object", status: 404, body: `{"detail": {"message": "no such route"}}`, want: "no such route"},
		{
			name:   "detail array",
			status: 422,
			body:   `{"detail": [{"loc": ["body", "questions", 0], "msg": "field required"}, {"loc": ["body", "model"], "msg": "not a string"}]}`,
			want:   "body.questions.0: field required; body.model: not a string",
		},
		{name: "unparseable", status: 500, body: `<html>gateway</html>`, want: "Internal Server Error"},
		{name: "empty", status: 418, body: ``, want: "I'm a teapot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(requestIDHeader, "req_err")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			client, _ := testClient(t, srv, WithMaxRetries(0))
			_, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")})

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("status = %d", apiErr.Status)
			}
			if apiErr.Message != tc.want {
				t.Errorf("message = %q, want %q", apiErr.Message, tc.want)
			}
			if apiErr.RequestID != "req_err" {
				t.Errorf("request id = %q", apiErr.RequestID)
			}
			if strings.Contains(apiErr.Error(), "test-key") {
				t.Error("error text leaks the api key")
			}
		})
	}
}

func TestRetryAfterMillisecondsHonoured(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("retry-after-ms", "120")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error": "slow down"}`)
			return
		}
		_, _ = io.WriteString(w, `{"model":"m","answers":{"a":{"type":"noul","noul":0.5}},"usage":{}}`)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv)
	if _, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d", got)
	}
	if len(sleeper.delays) != 1 || sleeper.delays[0] != 120*time.Millisecond {
		t.Errorf("delays = %v, want one of 120ms", sleeper.delays)
	}
}

func TestRetryAfterBeyondCapFallsBackToExponential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv)
	if _, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")}); err == nil {
		t.Fatal("want error after retries are spent")
	}
	want := []time.Duration{DefaultInitialBackoff, 2 * DefaultInitialBackoff}
	if len(sleeper.delays) != len(want) || sleeper.delays[0] != want[0] || sleeper.delays[1] != want[1] {
		t.Errorf("delays = %v, want %v", sleeper.delays, want)
	}
}

func TestBackoffCapAndJitter(t *testing.T) {
	client := New("k",
		WithBackoff(500*time.Millisecond, 5*time.Second),
		WithRandom(func() float64 { return 1 }),
	)
	full := New("k", WithBackoff(500*time.Millisecond, 5*time.Second), WithRandom(func() float64 { return 0 }))

	if got := full.backoff(4, errors.New("x")); got != 5*time.Second {
		t.Errorf("capped backoff = %v, want 5s", got)
	}
	if got := client.backoff(0, errors.New("x")); got != 375*time.Millisecond {
		t.Errorf("jittered backoff = %v, want 375ms", got)
	}
}

func TestRetryOnServerErrorThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"model":"m","answers":{"a":{"type":"noul","noul":0.7}},"usage":{}}`)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv)
	resp, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	if len(sleeper.delays) != 2 {
		t.Errorf("delays = %v", sleeper.delays)
	}
	if resp.Answers["a"].Prob() != 0.7 {
		t.Errorf("answer = %+v", resp.Answers["a"])
	}
}

func TestNoRetryOnBadRequest(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error": "bad state"}`)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv)
	if _, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")}); err == nil {
		t.Fatal("want error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if len(sleeper.delays) != 0 {
		t.Errorf("slept %v before giving up", sleeper.delays)
	}
}

func TestContextCancelDuringBackoff(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, sleeper := testClient(t, srv)
	sleeper.before = cancel

	_, err := client.Ask(ctx, "s", map[string]Question{"a": Noul("q", "", "")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestAttemptTimeoutTriggersRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// net/http only starts watching for a hang-up once the request
			// body has been consumed, so without this drain the handler
			// context would never be cancelled and the test would block until
			// the package timeout.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"model":"m","answers":{"a":{"type":"noul","noul":0.3}},"usage":{}}`)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv, WithAttemptTimeout(20*time.Millisecond))
	resp, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if len(sleeper.delays) != 1 {
		t.Errorf("delays = %v, want one", sleeper.delays)
	}
	if resp.Answers["a"].Prob() != 0.3 {
		t.Errorf("answer = %+v", resp.Answers["a"])
	}
}

func TestClientNeverPrintsTheKey(t *testing.T) {
	c := New("super-secret-key")
	printed := fmt.Sprintf("%v %+v %#v %s", c, c, c, c)
	if strings.Contains(printed, "super-secret-key") {
		t.Errorf("printing a client leaked the key: %s", printed)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		header map[string]string
		want   time.Duration
	}{
		{name: "none", header: map[string]string{}, want: 0},
		{name: "milliseconds", header: map[string]string{"retry-after-ms": "250"}, want: 250 * time.Millisecond},
		{
			name:   "milliseconds win",
			header: map[string]string{"retry-after-ms": "250", "Retry-After": "2"},
			want:   250 * time.Millisecond,
		},
		{name: "seconds", header: map[string]string{"Retry-After": "2"}, want: 2 * time.Second},
		{
			name:   "http date",
			header: map[string]string{"Retry-After": now.Add(90 * time.Second).Format(http.TimeFormat)},
			want:   90 * time.Second,
		},
		{
			name:   "http date in the past",
			header: map[string]string{"Retry-After": now.Add(-time.Minute).Format(http.TimeFormat)},
			want:   0,
		},
		{name: "unparseable", header: map[string]string{"Retry-After": "soon"}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.header {
				h.Set(k, v)
			}
			if got := retryAfter(h, now); got != tc.want {
				t.Errorf("retryAfter = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRetryAfterSecondsHonoured(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"model":"m","answers":{"a":{"type":"noul","noul":0.5}},"usage":{}}`)
	}))
	defer srv.Close()

	client, sleeper := testClient(t, srv)
	if _, err := client.Ask(context.Background(), "s", map[string]Question{"a": Noul("q", "", "")}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(sleeper.delays) != 1 || sleeper.delays[0] != 2*time.Second {
		t.Errorf("delays = %v, want one of 2s", sleeper.delays)
	}
}

func TestKeyFromEnv(t *testing.T) {
	t.Setenv(APIKeyEnv, "env-key")
	if got := KeyFromEnv(); got != "env-key" {
		t.Errorf("KeyFromEnv = %q", got)
	}
}
