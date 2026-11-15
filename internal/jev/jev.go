// Package jev is a client for the Jev System One wire API: POST
// /v1/systemone with a state and typed questions (noul, choice, score),
// answered with a probability for every option instead of generated text
// (ADR 0023). It speaks only the fields of TypeSafe's contract, so any
// server that implements it works by its base URL: TypeSafe's
// api.typesafe.ai, Codiv's api.codiv.ai, or a self-hosted OpenJev.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The defaults: TypeSafe's own endpoint and the TypeSafe SDKs' default
// model, which Jev-compatible servers accept as an alias of their newest
// model.
const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
)

// Question is one typed question. Criteria is a choice's options (a map
// from option name to its description, or nil when the name speaks for
// itself), a score's levels (a list, lowest first), or a noul's optional
// {"true": ..., "false": ...}.
type Question struct {
	Type         string `json:"type"` // "noul", "choice" or "score"
	Instructions string `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Options is a choice's criteria in a chosen order. A Go map would be
// written with its keys sorted ("1", "100", "102", ...), which reorders
// the options the model reads; a run on Wikipedia picked the 2nd option
// of that order with p=0.52.
type Options []Option

// Option is one choice option: its name and its description (nil when
// the name speaks for itself).
type Option struct {
	Name string
	Desc any
}

// MarshalJSON writes the options as one JSON object, in order.
func (o Options) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, opt := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(opt.Name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(opt.Desc)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Request is one System One request.
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"` // a string, or any JSON value
	Questions map[string]Question `json:"questions"`
}

// Answer is one question's answer: Choice for a choice, Noul (the
// probability of yes) for a noul, Score for a score; Probabilities covers
// every option of a choice or score and sums to 1.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

// Response is a System One response. Model names the exact version that
// answered.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// APIError is a non-2xx answer. Type is the server's error_type
// (rate_limit_error, quota_exceeded_error, overloaded_error, ...) when it
// sent one.
type APIError struct {
	Status    int
	Type      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Type != "" {
		return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Type, msg)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, msg)
}

// retryable reports an answer worth one more try: a rate limit or an
// overloaded server. A used-up quota is not (it stays used up).
func (e *APIError) retryable() bool {
	if e.Type == "quota_exceeded_error" || e.Type == "insufficient_quota" {
		return false
	}
	return e.Status == http.StatusTooManyRequests || e.Status == 529 || e.Status == http.StatusServiceUnavailable
}

// Client sends System One requests.
type Client struct {
	BaseURL string // without the /v1 path; a trailing slash is fine
	Key     string
	HTTP    *http.Client                               // nil: a client with no timeout of its own (the context bounds it)
	Sleep   func(context.Context, time.Duration) error // nil: a context-aware sleep; tests replace it
	Retries int                                        // extra tries on a rate limit, an overloaded server or a dropped connection; 0 means 2
}

// maxRetryAfter caps a server's retry-after: a pick waits seconds, not
// minutes.
const maxRetryAfter = 5 * time.Second

// SystemOne sends one request and returns the typed answers. A rate limit,
// an overloaded server (honouring retry-after, up to 5 s) or a dropped
// connection is retried; anything else fails at once.
func (c *Client) SystemOne(ctx context.Context, req Request) (*Response, error) {
	if c.Key == "" {
		return nil, errors.New("no API key")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	retries := c.Retries
	if retries == 0 {
		retries = 2
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/systemone"
	for attempt := 0; ; attempt++ {
		resp, wait, err := c.once(ctx, url, body)
		if err == nil {
			return resp, nil
		}
		var apiErr *APIError
		transient := errors.As(err, &apiErr) && apiErr.retryable() ||
			!errors.As(err, &apiErr) && ctx.Err() == nil
		if !transient || attempt >= retries {
			return nil, err
		}
		if wait == 0 {
			wait = time.Duration(attempt+1) * time.Second
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// once sends one attempt; wait is the server's retry-after, if any.
func (c *Client) once(ctx context.Context, url string, body []byte) (*Response, time.Duration, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	hreq.Header.Set("Authorization", "Bearer "+c.Key)
	hreq.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	hresp, err := hc.Do(hreq)
	if err != nil {
		return nil, 0, err
	}
	defer hresp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(hresp.Body, 16<<20))
	if err != nil {
		return nil, 0, err
	}
	if hresp.StatusCode < 200 || hresp.StatusCode > 299 {
		return nil, retryAfter(hresp.Header.Get("Retry-After")), apiError(hresp, raw)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, fmt.Errorf("unreadable response: %v", err)
	}
	return &out, 0, nil
}

// apiError reads Jev's error shapes: {"detail": {"error_type", "message"}},
// a 422's validation list under "detail", or a plain body.
func apiError(hresp *http.Response, raw []byte) *APIError {
	e := &APIError{Status: hresp.StatusCode, RequestID: hresp.Header.Get("x-typesafe-request-id")}
	var shaped struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(raw, &shaped) == nil && len(shaped.Detail) > 0 {
		var d struct {
			ErrorType string `json:"error_type"`
			Message   string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(shaped.Detail, &d) == nil && (d.ErrorType != "" || d.Message != ""):
			e.Type, e.Message = d.ErrorType, d.Message
		case json.Unmarshal(shaped.Detail, &s) == nil:
			e.Message = s
		default:
			e.Message = string(shaped.Detail)
		}
	} else {
		e.Message = strings.TrimSpace(string(raw))
	}
	if len(e.Message) > 300 {
		e.Message = e.Message[:300] + "..."
	}
	return e
}

// retryAfter parses a retry-after header in seconds, capped.
func retryAfter(v string) time.Duration {
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || n <= 0 {
		return 0
	}
	d := time.Duration(n * float64(time.Second))
	return min(d, maxRetryAfter)
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
