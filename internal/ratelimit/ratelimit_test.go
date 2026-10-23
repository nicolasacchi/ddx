package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two independent limiter instances on one state dir (what two ddx processes
// look like to the filesystem) must space their slots together.
func TestCrossInstanceSpacing(t *testing.T) {
	dir := t.TempDir()
	const interval = 80 * time.Millisecond
	a := &Limiter{Bucket: "b", Dir: dir, MinInterval: interval}
	b := &Limiter{Bucket: "b", Dir: dir, MinInterval: interval}

	var mu sync.Mutex
	var stamps []time.Time
	var wg sync.WaitGroup
	for _, l := range []*Limiter{a, b} {
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func(l *Limiter) {
				defer wg.Done()
				for i := 0; i < 3; i++ {
					if err := l.Wait(context.Background()); err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					stamps = append(stamps, time.Now())
					mu.Unlock()
				}
			}(l)
		}
	}
	wg.Wait()

	if len(stamps) != 12 {
		t.Fatalf("got %d stamps, want 12", len(stamps))
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].Before(stamps[j]) })
	for i := 1; i < len(stamps); i++ {
		// small tolerance for timer/scheduling skew
		if gap := stamps[i].Sub(stamps[i-1]); gap < interval-15*time.Millisecond {
			t.Fatalf("gap %d = %s, want >= ~%s", i, gap, interval)
		}
	}
	if total := stamps[len(stamps)-1].Sub(stamps[0]); total < 11*interval-30*time.Millisecond {
		t.Fatalf("12 slots took %s, want >= ~%s", total, 11*interval)
	}
}

func fakeLimiter(dir string, now *time.Time, slept *[]time.Duration) *Limiter {
	return &Limiter{
		Bucket: "f", Dir: dir, MinInterval: time.Second,
		Now: func() time.Time { return *now },
		Sleep: func(_ context.Context, d time.Duration) error {
			*slept = append(*slept, d)
			*now = now.Add(d)
			return nil
		},
	}
}

func TestHeaderDrivenWait(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_000_000, 0)
	var slept []time.Duration
	l := fakeLimiter(dir, &now, &slept)

	if err := l.Wait(context.Background()); err != nil || len(slept) != 0 {
		t.Fatalf("first Wait: err=%v slept=%v, want immediate", err, slept)
	}
	h := http.Header{}
	h.Set("x-ratelimit-remaining", "0")
	h.Set("x-ratelimit-reset", "17")
	if until := l.Observe(h); !until.Equal(now.Add(17 * time.Second)) {
		t.Fatalf("Observe until = %v", until)
	}

	// A different instance (another process) must also wait for the reset.
	var slept2 []time.Duration
	other := fakeLimiter(dir, &now, &slept2)
	if err := other.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(slept2) != 1 || slept2[0] != 17*time.Second {
		t.Fatalf("other instance slept %v, want [17s]", slept2)
	}
}

func TestObserveIgnoresRemaining(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var slept []time.Duration
	l := fakeLimiter(t.TempDir(), &now, &slept)
	h := http.Header{}
	h.Set("x-ratelimit-remaining", "12")
	h.Set("x-ratelimit-reset", "30")
	if !l.Observe(h).IsZero() {
		t.Fatal("remaining > 0 must not block")
	}
}

func TestNotifyOnlyWhenLong(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var slept []time.Duration
	l := fakeLimiter(t.TempDir(), &now, &slept)
	var notified []time.Duration
	l.Notify = func(d time.Duration) { notified = append(notified, d) }
	l.Wait(context.Background()) // immediate
	l.Wait(context.Background()) // 1s wait: below threshold
	if len(notified) != 0 {
		t.Fatalf("notified %v for a 1s wait", notified)
	}
	l.BlockFor(10 * time.Second)
	l.Wait(context.Background())
	if len(notified) != 1 {
		t.Fatalf("notified %v, want one note for the long wait", notified)
	}
}

func TestIntervalFromEnv(t *testing.T) {
	t.Setenv("X_INT", "")
	if d, err := IntervalFromEnv("X_INT", time.Second); err != nil || d != time.Second {
		t.Fatalf("unset: %v %v", d, err)
	}
	t.Setenv("X_INT", "250ms")
	if d, err := IntervalFromEnv("X_INT", time.Second); err != nil || d != 250*time.Millisecond {
		t.Fatalf("set: %v %v", d, err)
	}
	t.Setenv("X_INT", "fast")
	if _, err := IntervalFromEnv("X_INT", time.Second); err == nil {
		t.Fatal("want error for malformed value")
	}
}

func TestStateDirXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	if got := StateDir(); got != "/tmp/xdg-state/ddx/ratelimit" {
		t.Fatalf("StateDir = %q", got)
	}
}

// A block learned by process A while B is already sleeping on a reserved slot
// must extend B's wait instead of letting B fire into a 429.
func TestBlockReachesSleepingWaiter(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_000_000, 0)
	a := fakeLimiter(dir, &now, new([]time.Duration))
	var slept []time.Duration
	b := fakeLimiter(dir, &now, &slept)
	b.Wait(context.Background()) // takes slot at now; next = now+1s
	first := true
	b.Sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		now = now.Add(d)
		if first { // while B sleeps, A sees remaining=0
			first = false
			if err := a.BlockFor(20 * time.Second); err != nil {
				t.Error(err)
			}
		}
		return nil
	}
	start := now
	if err := b.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := now.Sub(start); got < 20*time.Second {
		t.Fatalf("B returned after %s (slept %v), want >= the 20s block", got, slept)
	}
}

func TestStaleFutureStateIsClamped(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_000_000, 0)
	var slept []time.Duration
	l := fakeLimiter(dir, &now, &slept)
	// A bogus 1h reset must be clamped to MaxBlock on the way in...
	if err := l.BlockFor(time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(slept) == 0 || slept[0] > MaxBlock {
		t.Fatalf("slept %v, want <= %s", slept, MaxBlock)
	}
	// ...and stale state already on disk (clock jump) is clamped on the way out.
	l2 := fakeLimiter(t.TempDir(), &now, &slept)
	lock, st, _ := l2.paths()
	_ = lock
	os.WriteFile(st, []byte(`{"next_unix_nano":`+strconv.FormatInt(now.Add(24*time.Hour).UnixNano(), 10)+`}`), 0o600)
	slept = nil
	if err := l2.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(slept) == 0 || slept[0] > MaxBlock {
		t.Fatalf("stale state: slept %v, want <= %s", slept, MaxBlock)
	}
}

func TestCorruptStateResets(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var slept []time.Duration
	l := fakeLimiter(t.TempDir(), &now, &slept)
	_, st, _ := l.paths()
	os.WriteFile(st, []byte("{not json"), 0o600)
	if err := l.Wait(context.Background()); err != nil || len(slept) != 0 {
		t.Fatalf("err=%v slept=%v, want immediate slot", err, slept)
	}
}

func TestWaitHonorsContextCancel(t *testing.T) {
	l := &Limiter{Bucket: "c", Dir: t.TempDir(), MinInterval: time.Hour}
	l.Wait(context.Background()) // slot now; next in 1h (clamped to 2m)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("want ctx error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Wait ignored ctx cancel")
	}
}

// Real separate OS processes sharing one state dir must space their slots.
func TestCrossProcessSpacing(t *testing.T) {
	if os.Getenv("DDX_RL_HELPER_DIR") != "" {
		return
	}
	dir := t.TempDir()
	const interval = 100 * time.Millisecond
	type out struct {
		b   []byte
		err error
	}
	ch := make(chan out, 3)
	for i := 0; i < 3; i++ {
		go func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
			cmd.Env = append(os.Environ(), "DDX_RL_HELPER_DIR="+dir)
			b, err := cmd.Output()
			ch <- out{b, err}
		}()
	}
	var stamps []int64
	for i := 0; i < 3; i++ {
		o := <-ch
		if o.err != nil {
			t.Fatal(o.err)
		}
		for _, f := range strings.Fields(string(o.b)) {
			if n, err := strconv.ParseInt(f, 10, 64); err == nil && n > 1e15 {
				stamps = append(stamps, n)
			}
		}
	}
	if len(stamps) != 12 {
		t.Fatalf("got %d stamps, want 12", len(stamps))
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i] < stamps[j] })
	for i := 1; i < len(stamps); i++ {
		if gap := time.Duration(stamps[i] - stamps[i-1]); gap < interval-20*time.Millisecond {
			t.Fatalf("gap %d = %s, want >= ~%s", i, gap, interval)
		}
	}
}

func TestHelperProcess(t *testing.T) {
	dir := os.Getenv("DDX_RL_HELPER_DIR")
	if dir == "" {
		t.Skip("helper")
	}
	l := &Limiter{Bucket: "x", Dir: dir, MinInterval: 100 * time.Millisecond}
	for i := 0; i < 4; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		fmt.Println(time.Now().UnixNano())
	}
}
