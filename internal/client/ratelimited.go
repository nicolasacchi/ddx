package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nicolasacchi/clicore/redact"
	"github.com/nicolasacchi/ddx/internal/ratelimit"
)

// RateLimitedMaxTries bounds the attempts (first try + retries) of a rate-limited call.
const RateLimitedMaxTries = 8

// internalErrorMaxTries bounds attempts on a plain HTTP 500.
const internalErrorMaxTries = 3

// snippet returns a short, single-line excerpt of an error body.
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if s == "" {
		return "(empty body)"
	}
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// SetRetryNotify registers a callback for retry waits longer than 2s.
func (c *Client) SetRetryNotify(f func(d time.Duration, why string)) { c.retryNotify = f }

// RateLimitScope identifies the server-side rate-limit domain of this client
// (API base URL + app key: Datadog budgets are per user). It contains the app
// key, so only ever hash it (ratelimit.PABucketFor), never print or store it.
func (c *Client) RateLimitScope() string { return c.baseURL + "\x00" + c.appKey }

// SetBaseURL overrides the API base URL (used by tests to target httptest servers).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// PostRateLimited is Post for endpoints under a shared cross-process rate limit
// (e.g. Product Analytics, 60 req/min per user). Every attempt first takes a
// slot from rl; response x-ratelimit-* headers are fed back into rl. Retry policy:
//
//   - 429: block the whole bucket until x-ratelimit-reset (+ jitter), retry
//   - 5xx / network / timeout: exponential backoff (1s, 2s, 4s ... capped at 30s), retry
//   - other 4xx: return the API error immediately
//
// At most RateLimitedMaxTries attempts are made.
func (c *Client) PostRateLimited(ctx context.Context, path string, body any, rl *ratelimit.Limiter) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return c.doRateLimited(ctx, http.MethodPost, c.buildURL(path, nil), buf.Bytes(), rl)
}

func (c *Client) doRateLimited(ctx context.Context, method, rawURL string, bodyBytes []byte, rl *ratelimit.Limiter) (json.RawMessage, error) {
	if c.verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", method, redact.URL(rawURL))
		if len(bodyBytes) > 0 {
			fmt.Fprintf(os.Stderr, "> Body: %s\n", redact.Body(bodyBytes))
		}
	}

	var lastErr error
	for attempt := 0; attempt < RateLimitedMaxTries; attempt++ {
		if err := rl.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("request failed: %w", err)
			if err := c.backoff(ctx, attempt, lastErr.Error()); err != nil {
				return nil, err
			}
			continue
		}
		respBody, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if c.verbose {
			fmt.Fprintf(os.Stderr, "< %d %s (%s, %s)\n", resp.StatusCode, http.StatusText(resp.StatusCode), time.Since(start).Round(time.Millisecond), humanBytes(len(respBody)))
		}

		// Share server-side window state with every process on this bucket.
		rl.Observe(resp.Header)

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			wait, ok := ratelimit.ResetAfter(resp.Header)
			if !ok {
				wait = time.Second << min(attempt, 5)
			}
			wait += ratelimit.Jitter()
			lastErr = &APIError{StatusCode: 429, Title: "Too Many Requests", Detail: fmt.Sprintf("rate limited after %d attempts", attempt+1), Hint: hintForError(429)}
			if c.verbose {
				fmt.Fprintf(os.Stderr, "! 429, bucket blocked for %s (attempt %d/%d)\n", wait.Round(time.Millisecond), attempt+1, RateLimitedMaxTries)
			}
			if err := rl.BlockFor(wait); err != nil {
				return nil, err
			}
		case resp.StatusCode >= 500 || rerr != nil:
			lastErr = &APIError{StatusCode: resp.StatusCode, Title: http.StatusText(resp.StatusCode), Detail: fmt.Sprintf("failed after %d attempts: %s", attempt+1, snippet(respBody))}
			if rerr != nil {
				lastErr = fmt.Errorf("read response: %w", rerr)
			}
			// A plain 500 is usually deterministic (e.g. a window the API
			// cannot serve); only gateway-ish errors deserve the full budget.
			if resp.StatusCode == http.StatusInternalServerError && rerr == nil && attempt+1 >= internalErrorMaxTries {
				return nil, lastErr
			}
			if err := c.backoff(ctx, attempt, fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))); err != nil {
				return nil, err
			}
		case resp.StatusCode < 200 || resp.StatusCode >= 300:
			if c.verbose {
				fmt.Fprintf(os.Stderr, "< Body: %s\n", redact.Body(respBody))
			}
			return nil, c.parseError(respBody, resp.StatusCode)
		default:
			if len(respBody) == 0 {
				return json.RawMessage("{}"), nil
			}
			return json.RawMessage(respBody), nil
		}
	}
	return nil, lastErr
}

// backoff sleeps 1s,2s,4s... (cap 30s) plus jitter; it is skipped after the
// last attempt since the caller is about to give up.
func (c *Client) backoff(ctx context.Context, attempt int, why string) error {
	if attempt == RateLimitedMaxTries-1 {
		return nil
	}
	d := min(time.Second<<attempt, 30*time.Second) + ratelimit.Jitter()
	if c.verbose {
		fmt.Fprintf(os.Stderr, "! %s, retrying in %s (attempt %d/%d)\n", why, d.Round(time.Millisecond), attempt+1, RateLimitedMaxTries)
	}
	if c.retryNotify != nil && d > 2*time.Second {
		c.retryNotify(d, why)
	}
	if c.backoffSleep != nil {
		return c.backoffSleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetBackoffSleep overrides the retry sleep of rate-limited calls (tests).
func (c *Client) SetBackoffSleep(f func(ctx context.Context, d time.Duration) error) {
	c.backoffSleep = f
}
