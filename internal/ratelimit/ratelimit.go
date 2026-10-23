// Package ratelimit is a small cross-process rate limiter. N concurrent ddx
// processes (parallel agents) that share a bucket name coordinate through a
// flock-protected state file so that, together, they never exceed the bucket
// rate. It also absorbs server-side hints (x-ratelimit-* headers, 429s) into
// the shared state so every process backs off at once.
package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PABucket is the bucket shared by every Product Analytics analytics call
// (Datadog: analytics_extended_per_user, 60 requests/min per user).
const PABucket = "product-analytics"

// DefaultPAInterval is the minimum spacing for the PA bucket (60/min).
const DefaultPAInterval = time.Second

// state is the persisted shared state: the earliest instant (unix nanoseconds)
// at which the next request may be sent.
type state struct {
	NextUnixNano int64 `json:"next_unix_nano"`
	// BlockUnixNano is a server-driven hard block (429 / remaining=0): unlike
	// NextUnixNano it is re-checked by callers that already hold a slot, so a
	// block learned by one process reaches processes that are mid-sleep.
	BlockUnixNano int64 `json:"block_unix_nano,omitempty"`
}

// MaxBlock caps how far into the future any persisted or server-supplied block
// may reach. A 60/min window never needs more than ~60s, so anything larger is
// a clock jump or a bogus header and must not wedge the bucket across runs.
const MaxBlock = 2 * time.Minute

// Limiter hands out request slots at most one per MinInterval, across processes.
// The zero value is not usable; build one with New.
type Limiter struct {
	Bucket      string
	Dir         string        // state directory; defaults to StateDir()
	MinInterval time.Duration // minimum spacing between slots
	// Notify, if set, is called before sleeping when the wait exceeds
	// NotifyAfter (default 2s).
	Notify      func(wait time.Duration)
	NotifyAfter time.Duration

	// Now and Sleep are test seams.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	mu sync.Mutex // serializes in-process callers (flock is per open file description)
}

// New returns a limiter for bucket with the given minimum spacing, using the
// default state directory.
func New(bucket string, minInterval time.Duration) *Limiter {
	return &Limiter{Bucket: bucket, MinInterval: minInterval}
}

// StateDir returns $XDG_STATE_HOME/ddx/ratelimit, falling back to
// ~/.local/state/ddx/ratelimit (and the OS temp dir if there is no home).
func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "ddx", "ratelimit")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "ddx", "ratelimit")
	}
	return filepath.Join(os.TempDir(), "ddx-ratelimit")
}

// IntervalFromEnv parses a Go duration from env var name; def is returned when
// unset. A malformed or negative value is an error rather than silently
// ignored. "0" disables spacing.
func IntervalFromEnv(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s=%q: expected a non-negative Go duration like 1s or 500ms", name, v)
	}
	return d, nil
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Limiter) dir() string {
	if l.Dir != "" {
		return l.Dir
	}
	return StateDir()
}

func (l *Limiter) paths() (lock, st string, err error) {
	d := l.dir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", "", fmt.Errorf("ratelimit state dir: %w", err)
	}
	name := sanitize(l.Bucket)
	return filepath.Join(d, name+".lock"), filepath.Join(d, name+".json"), nil
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
}

// update runs fn under the cross-process lock with the current state and
// persists whatever fn leaves in it.
func (l *Limiter) update(fn func(s *state)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	lockPath, statePath, err := l.paths()
	if err != nil {
		return err
	}
	unlock, err := lockFile(lockPath)
	if err != nil {
		return fmt.Errorf("ratelimit lock: %w", err)
	}
	defer unlock()

	var s state
	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, &s) // a corrupt file just resets the bucket
	}
	fn(&s)
	b, _ := json.Marshal(s)
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("ratelimit state: %w", err)
	}
	return os.Rename(tmp, statePath)
}

// Wait reserves the next free slot and sleeps until it arrives. The slot is
// reserved under the lock but the sleep happens outside it, so waiting
// processes queue up in reservation order without holding the lock. After the
// sleep the shared state is re-read: if another process learned of a server
// block meanwhile (429 / remaining=0), the wait is extended and the slot is
// re-reserved instead of firing into a known 429.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		var slot time.Time
		err := l.update(func(s *state) {
			now := l.now()
			l.clamp(s, now)
			slot = now
			if next := time.Unix(0, s.NextUnixNano); next.After(slot) {
				slot = next
			}
			s.NextUnixNano = slot.Add(l.MinInterval).UnixNano()
		})
		if err != nil {
			return err
		}
		if wait := slot.Sub(l.now()); wait > 0 {
			after := l.NotifyAfter
			if after == 0 {
				after = 2 * time.Second
			}
			if l.Notify != nil && wait > after {
				l.Notify(wait)
			}
			if err := l.sleep(ctx, wait); err != nil {
				return err
			}
		}
		// Did a block land while we slept?
		var blockedUntil time.Time
		if err := l.update(func(s *state) {
			now := l.now()
			l.clamp(s, now)
			if b := time.Unix(0, s.BlockUnixNano); s.BlockUnixNano != 0 && b.After(now) {
				blockedUntil = b
			}
		}); err != nil {
			return err
		}
		if blockedUntil.IsZero() {
			return nil
		}
		if d := blockedUntil.Sub(l.now()); d > 0 {
			if err := l.sleep(ctx, d); err != nil {
				return err
			}
		}
	}
}

// clamp drops absurd future timestamps (clock jump, bogus header) left in the state.
func (l *Limiter) clamp(s *state, now time.Time) {
	limit := now.Add(MaxBlock).UnixNano()
	if s.NextUnixNano > limit {
		s.NextUnixNano = limit
	}
	if s.BlockUnixNano > limit {
		s.BlockUnixNano = limit
	}
}

func (l *Limiter) sleep(ctx context.Context, d time.Duration) error {
	if l.Sleep != nil {
		return l.Sleep(ctx, d)
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

// BlockUntil pushes the shared next-allowed time out to at least t, making
// every process using this bucket wait.
func (l *Limiter) BlockUntil(t time.Time) error {
	return l.update(func(s *state) {
		now := l.now()
		if t.After(now.Add(MaxBlock)) {
			t = now.Add(MaxBlock)
		}
		n := t.UnixNano()
		if n > s.NextUnixNano {
			s.NextUnixNano = n
		}
		if n > s.BlockUnixNano {
			s.BlockUnixNano = n
		}
	})
}

// BlockFor is BlockUntil(now + d) on the limiter's clock.
func (l *Limiter) BlockFor(d time.Duration) error { return l.BlockUntil(l.now().Add(d)) }

// Observe absorbs x-ratelimit-remaining / x-ratelimit-reset (seconds until the
// window resets) from a response. When no requests remain, the whole bucket is
// blocked until the reset. It returns the block deadline (zero if none).
func (l *Limiter) Observe(h http.Header) time.Time {
	rem, err := strconv.Atoi(strings.TrimSpace(h.Get("x-ratelimit-remaining")))
	if err != nil || rem > 0 {
		return time.Time{}
	}
	reset, ok := ResetAfter(h)
	if !ok {
		return time.Time{}
	}
	until := l.now().Add(reset)
	_ = l.BlockUntil(until)
	return until
}

// ResetAfter parses x-ratelimit-reset (seconds until reset). Falls back to
// Retry-After.
func ResetAfter(h http.Header) (time.Duration, bool) {
	for _, k := range []string{"x-ratelimit-reset", "Retry-After"} {
		if v := strings.TrimSpace(h.Get(k)); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
				return time.Duration(f * float64(time.Second)), true
			}
		}
	}
	return 0, false
}

// Jitter returns a small random delay (0-250ms) used to de-synchronize
// processes released by the same reset.
func Jitter() time.Duration {
	return time.Duration(rand.IntN(250)) * time.Millisecond
}
