package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nicolasacchi/ddx/internal/ratelimit"
)

func testLimiter(t *testing.T, slept *[]time.Duration) *ratelimit.Limiter {
	t.Helper()
	now := time.Unix(1_000_000, 0)
	return &ratelimit.Limiter{
		Bucket: "t", Dir: t.TempDir(), MinInterval: 0,
		Now: func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			*slept = append(*slept, d)
			now = now.Add(d)
			return nil
		},
	}
}

func TestPostRateLimited429HonorsReset(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("x-ratelimit-reset", "7")
			w.Header().Set("x-ratelimit-remaining", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	got, err := c.PostRateLimited(context.Background(), "x", map[string]any{"a": 1}, testLimiter(t, &slept))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"ok":true}` || calls.Load() != 2 {
		t.Fatalf("got %s after %d calls", got, calls.Load())
	}
	if len(slept) != 1 || slept[0] < 7*time.Second || slept[0] > 8*time.Second {
		t.Fatalf("slept %v, want ~7s (+<=250ms jitter) from x-ratelimit-reset", slept)
	}
}

func TestPostRateLimitedGivesUpAfterMaxTries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("x-ratelimit-reset", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	var slept []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	_, err := c.PostRateLimited(context.Background(), "x", map[string]any{}, testLimiter(t, &slept))
	if err == nil || calls.Load() != RateLimitedMaxTries {
		t.Fatalf("err=%v calls=%d, want error after %d tries", err, calls.Load(), RateLimitedMaxTries)
	}
	if ae, ok := err.(*APIError); !ok || ae.ExitCode() != 5 {
		t.Fatalf("want rate_limited APIError (exit 5), got %#v", err)
	}
}

func TestPostRateLimited5xxBackoffThenOK(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var slept, backoffs []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	c.SetBackoffSleep(func(_ context.Context, d time.Duration) error { backoffs = append(backoffs, d); return nil })
	if _, err := c.PostRateLimited(context.Background(), "x", map[string]any{}, testLimiter(t, &slept)); err != nil {
		t.Fatal(err)
	}
	if len(backoffs) != 2 || backoffs[0] < time.Second || backoffs[1] < 2*time.Second || backoffs[1] < backoffs[0] {
		t.Fatalf("backoffs %v, want exponential 1s,2s (+jitter)", backoffs)
	}
}

func TestPostRateLimited4xxReturnsAPIBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":[{"detail":"limit must be lte 1000"}]}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	_, err := c.PostRateLimited(context.Background(), "x", map[string]any{}, testLimiter(t, &slept))
	ae, ok := err.(*APIError)
	if !ok || ae.StatusCode != 400 || ae.Detail != "limit must be lte 1000" || calls.Load() != 1 {
		t.Fatalf("err=%#v calls=%d", err, calls.Load())
	}
}

func TestPostRateLimited500KeepsBodyAndStopsEarly(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":["window too small"]}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	c.SetBackoffSleep(func(context.Context, time.Duration) error { return nil })
	_, err := c.PostRateLimited(context.Background(), "x", map[string]any{}, testLimiter(t, &slept))
	ae, ok := err.(*APIError)
	if !ok || ae.StatusCode != 500 || !strings.Contains(ae.Detail, "window too small") || calls.Load() != internalErrorMaxTries {
		t.Fatalf("err=%#v calls=%d", err, calls.Load())
	}
}

func TestPostRateLimited429WithoutResetFallsBackToBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := New("k", "a", "datadoghq.eu", false)
	c.SetBaseURL(srv.URL)
	if _, err := c.PostRateLimited(context.Background(), "x", map[string]any{}, testLimiter(t, &slept)); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] < time.Second {
		t.Fatalf("slept %v, want >= 1s fallback", slept)
	}
}
