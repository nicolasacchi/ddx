package commands

import (
	"encoding/json"
	"errors"
	"testing"
)

// helpersFakePage builds n fake JSON items, numbered from start.
func helpersFakePage(start, n int) []json.RawMessage {
	items := make([]json.RawMessage, n)
	for i := 0; i < n; i++ {
		items[i] = json.RawMessage(`{"id":` + helpersItoa(start+i) + `}`)
	}
	return items
}

func helpersItoa(n int) string {
	// Avoid pulling in strconv just for test fixtures.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func TestPaginateOffsetExactMultiple(t *testing.T) {
	const size = 10
	const total = 20
	calls := 0

	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		calls++
		if sz != size {
			t.Fatalf("expected size %d, got %d", size, sz)
		}
		remaining := total - offset
		n := sz
		if remaining < n {
			n = remaining
		}
		if n < 0 {
			n = 0
		}
		return helpersFakePage(offset, n), total, nil
	}

	items, gotTotal, err := paginateOffset(fetch, size, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTotal != total {
		t.Fatalf("total = %d, want %d", gotTotal, total)
	}
	if len(items) != total {
		t.Fatalf("len(items) = %d, want %d", len(items), total)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (20/10)", calls)
	}
}

func TestPaginateOffsetShortLastPage(t *testing.T) {
	const size = 10
	const total = 25
	calls := 0

	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		calls++
		remaining := total - offset
		n := sz
		if remaining < n {
			n = remaining
		}
		if n < 0 {
			n = 0
		}
		return helpersFakePage(offset, n), total, nil
	}

	items, gotTotal, err := paginateOffset(fetch, size, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTotal != total {
		t.Fatalf("total = %d, want %d", gotTotal, total)
	}
	if len(items) != total {
		t.Fatalf("len(items) = %d, want %d", len(items), total)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (10,10,5)", calls)
	}
}

func TestPaginateOffsetMaxPagesCap(t *testing.T) {
	const size = 10
	calls := 0

	// Simulate an API that never reports a usable total and always returns a
	// full page — without the maxPages cap this would loop forever.
	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		calls++
		return helpersFakePage(offset, sz), 0, nil
	}

	items, gotTotal, err := paginateOffset(fetch, size, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (maxPages cap)", calls)
	}
	if len(items) != size*3 {
		t.Fatalf("len(items) = %d, want %d", len(items), size*3)
	}
	if gotTotal != 0 {
		t.Fatalf("total = %d, want 0 (never reported)", gotTotal)
	}
}

func TestPaginateOffsetFetchErrorMidway(t *testing.T) {
	const size = 10
	calls := 0
	fetchErr := errors.New("boom: 500 from upstream")

	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		calls++
		if calls == 1 {
			return helpersFakePage(offset, sz), 100, nil
		}
		return nil, 0, fetchErr
	}

	items, gotTotal, err := paginateOffset(fetch, size, 20)
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want %v", err, fetchErr)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	// Items and total accumulated from the first successful call must survive
	// the second call's failure.
	if len(items) != size {
		t.Fatalf("len(items) = %d, want %d (first page retained)", len(items), size)
	}
	if gotTotal != 100 {
		t.Fatalf("total = %d, want 100 (from last successful fetch)", gotTotal)
	}
}
