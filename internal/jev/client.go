package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zoidsh/tenet/internal/buildinfo"
)

// Defaults, which mirror the official JS SDK so that behaviour under load
// matches what TypeSafe documents.
const (
	DefaultBaseURL        = "https://api.typesafe.ai"
	DefaultModel          = "jev-1.13.0"
	DefaultMaxRetries     = 2
	DefaultAttemptTimeout = 10 * time.Second
	DefaultInitialBackoff = 500 * time.Millisecond
	DefaultMaxBackoff     = 5 * time.Second
	DefaultJitter         = 0.25
	DefaultMaxRetryAfter  = 60 * time.Second
)

// APIKeyEnv is where KeyFromEnv looks.
const APIKeyEnv = "TYPESAFE_API_KEY"

// BaseURLEnv points the client at another host, for a proxy, a recording or a
// local stand-in.
const BaseURLEnv = "TYPESAFE_BASE_URL"

const requestIDHeader = "x-typesafe-request-id"

// options configure a Client. Every field has a default; the last few are
// seams the tests drive so they need not wait in real time.
type options struct {
	BaseURL        string
	Model          string
	UserAgent      string
	HTTPClient     *http.Client
	MaxRetries     int
	AttemptTimeout time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         float64
	MaxRetryAfter  time.Duration
	Sleep          func(ctx context.Context, d time.Duration) error
	Random         func() float64
	Now            func() time.Time
}

// Option overrides one default.
type Option func(*options)

// WithBaseURL points the client at another API host.
func WithBaseURL(url string) Option {
	return func(o *options) { o.BaseURL = strings.TrimRight(url, "/") }
}

// WithModel picks the jev model version to ask.
func WithModel(model string) Option {
	return func(o *options) { o.Model = model }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(o *options) { o.UserAgent = ua }
}

// WithHTTPClient supplies the transport to send requests on.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.HTTPClient = c }
}

// WithMaxRetries sets how many attempts follow the first one.
func WithMaxRetries(n int) Option {
	return func(o *options) { o.MaxRetries = n }
}

// WithAttemptTimeout bounds a single attempt.
func WithAttemptTimeout(d time.Duration) Option {
	return func(o *options) { o.AttemptTimeout = d }
}

// WithBackoff sets the first retry delay and the ceiling it doubles towards.
func WithBackoff(initial, ceiling time.Duration) Option {
	return func(o *options) { o.InitialBackoff, o.MaxBackoff = initial, ceiling }
}

// WithJitter sets the fraction of a backoff that is subtracted at random.
func WithJitter(fraction float64) Option {
	return func(o *options) { o.Jitter = fraction }
}

// WithMaxRetryAfter sets the longest server-named delay worth waiting out.
func WithMaxRetryAfter(d time.Duration) Option {
	return func(o *options) { o.MaxRetryAfter = d }
}

// WithSleep replaces the wait between attempts.
func WithSleep(sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(o *options) { o.Sleep = sleep }
}

// WithRandom replaces the source of jitter.
func WithRandom(random func() float64) Option {
	return func(o *options) { o.Random = random }
}

// WithNow replaces the clock that resolves an HTTP-date Retry-After.
func WithNow(now func() time.Time) Option {
	return func(o *options) { o.Now = now }
}

// Client talks to the jev API.
type Client struct {
	apiKey string
	opts   options
}

// String and GoString exist because fmt reaches unexported fields by
// reflection: without them, printing a Client with %v, %+v or %#v would spill
// the API key into a log.
func (c *Client) String() string { return "jev.Client" }

// GoString keeps %#v from reaching the key, as String does for %v and %+v.
func (c *Client) GoString() string { return "jev.Client" }

// KeyFromEnv reads the API key from the environment.
func KeyFromEnv() string { return os.Getenv(APIKeyEnv) }

// New builds a client for one API key.
func New(apiKey string, opts ...Option) *Client {
	o := options{
		BaseURL:        DefaultBaseURL,
		Model:          DefaultModel,
		UserAgent:      "tenet/" + buildinfo.Version(),
		HTTPClient:     http.DefaultClient,
		MaxRetries:     DefaultMaxRetries,
		AttemptTimeout: DefaultAttemptTimeout,
		InitialBackoff: DefaultInitialBackoff,
		MaxBackoff:     DefaultMaxBackoff,
		Jitter:         DefaultJitter,
		MaxRetryAfter:  DefaultMaxRetryAfter,
		Sleep:          sleep,
		Random:         rand.Float64,
		Now:            time.Now,
	}
	if url := os.Getenv(BaseURLEnv); url != "" {
		WithBaseURL(url)(&o)
	}
	// The options come last so that a caller that names a host outranks the
	// environment.
	for _, apply := range opts {
		apply(&o)
	}
	return &Client{apiKey: apiKey, opts: o}
}

type request struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Ask puts every question to the model in one call, which is what makes
// fan-out cheap: the questions are evaluated against the same state together.
func (c *Client) Ask(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	if err := validateQuestions(questions); err != nil {
		return nil, err
	}
	body, err := json.Marshal(request{State: state, Model: c.opts.Model, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("jev: encoding request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.attempt(ctx, body)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt >= c.opts.MaxRetries || !retryable(err) {
			return nil, err
		}
		if err := c.opts.Sleep(ctx, c.backoff(attempt, err)); err != nil {
			return nil, err
		}
	}
}

func (c *Client) attempt(ctx context.Context, body []byte) (*Response, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.opts.BaseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jev: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.opts.UserAgent)

	httpResp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, &transportError{err: err}
	}
	defer func() { _ = httpResp.Body.Close() }()

	payload, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &transportError{err: err}
	}

	requestID := httpResp.Header.Get(requestIDHeader)
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return nil, &APIError{
			Status:     httpResp.StatusCode,
			Message:    errorMessage(payload, httpResp.StatusCode),
			RequestID:  requestID,
			RetryAfter: retryAfter(httpResp.Header, c.opts.Now()),
		}
	}

	var resp Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("jev: decoding response: %w", err)
	}
	resp.RequestID = requestID
	return &resp, nil
}

// transportError marks a connection failure or a per-attempt timeout, both of
// which are worth another attempt.
type transportError struct{ err error }

func (e *transportError) Error() string { return "jev: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func retryable(err error) bool {
	var transport *transportError
	if errors.As(err, &transport) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return false
}

// backoff is exponential with subtractive jitter, unless the server named a
// delay it is willing to wait out.
func (c *Client) backoff(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 && apiErr.RetryAfter <= c.opts.MaxRetryAfter {
		return apiErr.RetryAfter
	}
	d := c.opts.InitialBackoff << attempt
	if d > c.opts.MaxBackoff || d <= 0 {
		d = c.opts.MaxBackoff
	}
	return time.Duration(float64(d) * (1 - c.opts.Jitter*c.opts.Random()))
}

func retryAfter(h http.Header, now time.Time) time.Duration {
	if ms := h.Get("retry-after-ms"); ms != "" {
		if v, err := strconv.ParseFloat(ms, 64); err == nil && v >= 0 {
			return time.Duration(v * float64(time.Millisecond))
		}
	}
	value := h.Get("Retry-After")
	if value == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(value, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if date, err := http.ParseTime(value); err == nil {
		if d := date.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
