package commands

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestIrParseHostsPage(t *testing.T) {
	raw := json.RawMessage(`{
		"host_list": [
			{"id": 1, "host_name": "i-one"},
			{"id": 2, "host_name": "i-two"}
		],
		"total_matching": 2,
		"total_returned": 2
	}`)

	hosts, total, err := irParseHostsPage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(hosts) != 2 {
		t.Fatalf("len(hosts) = %d, want 2", len(hosts))
	}
}

func TestIrParseHostsPageEmpty(t *testing.T) {
	raw := json.RawMessage(`{"host_list": [], "total_matching": 0, "total_returned": 0}`)
	hosts, total, err := irParseHostsPage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if len(hosts) != 0 {
		t.Fatalf("len(hosts) = %d, want 0", len(hosts))
	}
}

func TestIrParseHostsPageMalformed(t *testing.T) {
	if _, _, err := irParseHostsPage(json.RawMessage(`not json`)); err == nil {
		t.Fatalf("expected error on malformed input")
	}
}

// TestIrHostsAllPagination exercises paginateOffset (owned jointly, per the
// task brief, but with no other round-A consumer) wired to the v1 hosts
// start/count/total_matching shape via a fake fetch func — no network.
func TestIrHostsAllPagination(t *testing.T) {
	const total = 25
	const pageSize = 10
	calls := 0

	fetch := func(offset, size int) ([]json.RawMessage, int, error) {
		calls++
		remaining := total - offset
		n := size
		if remaining < n {
			n = remaining
		}
		if n < 0 {
			n = 0
		}
		items := make([]json.RawMessage, n)
		for i := 0; i < n; i++ {
			items[i] = json.RawMessage(`{"id": ` + irItoaHosts(offset+i) + `}`)
		}
		return items, total, nil
	}

	items, gotTotal, err := paginateOffset(fetch, pageSize, 20)
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

func TestIrHostsAllPaginationFetchError(t *testing.T) {
	boom := errors.New("boom")
	fetch := func(offset, size int) ([]json.RawMessage, int, error) {
		return nil, 0, boom
	}
	_, _, err := paginateOffset(fetch, 10, 20)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
}

func irItoaHosts(n int) string {
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
