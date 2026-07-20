package commands

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDriftInvestigationsMaxPages(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		size  int
		want  int
	}{
		{"no limit means one page", 0, 100, 1},
		{"negative limit means one page", -5, 100, 1},
		{"limit under size means one page", 50, 100, 1},
		{"limit equal to size means one page", 100, 100, 1},
		{"limit just over size means two pages", 101, 100, 2},
		{"exact multiple", 300, 100, 3},
		{"non-multiple rounds up", 250, 100, 3},
		{"zero size guarded", 50, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := driftInvestigationsMaxPages(tt.limit, tt.size)
			if got != tt.want {
				t.Fatalf("driftInvestigationsMaxPages(%d, %d) = %d, want %d", tt.limit, tt.size, got, tt.want)
			}
		})
	}
}

func TestDriftUnwrapInvestigationsPage(t *testing.T) {
	raw := json.RawMessage(`{
		"data": [
			{"id": "a1", "type": "investigation", "attributes": {"status": "conclusive", "title": "t1"}},
			{"id": "a2", "type": "investigation", "attributes": {"status": "in_progress", "title": "t2"}}
		],
		"meta": {"page": {"limit": 10, "offset": 0, "total": 50}}
	}`)

	items, total, err := driftUnwrapInvestigationsPage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 50 {
		t.Fatalf("total = %d, want 50", total)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestDriftUnwrapInvestigationsPageNoMeta(t *testing.T) {
	raw := json.RawMessage(`{"data": [{"id": "a1", "type": "investigation", "attributes": {"status": "conclusive"}}]}`)

	items, total, err := driftUnwrapInvestigationsPage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0 (no meta reported)", total)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
}

func TestDriftUnwrapInvestigationsPageMalformed(t *testing.T) {
	_, _, err := driftUnwrapInvestigationsPage(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}

// TestDriftUnwrapInvestigationsPageWithPaginateOffset exercises the real
// consumer, paginateOffset, driving driftUnwrapInvestigationsPage-shaped
// fetches across multiple simulated pages — verifying the two functions
// compose the way investigationsListCmd relies on, without touching the
// network.
func TestDriftUnwrapInvestigationsPageWithPaginateOffset(t *testing.T) {
	const size = 2
	const total = 5
	calls := 0

	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		calls++
		if sz != size {
			t.Fatalf("expected page size %d, got %d", size, sz)
		}
		remaining := total - offset
		n := sz
		if remaining < n {
			n = remaining
		}
		if n < 0 {
			n = 0
		}
		items := make([]json.RawMessage, n)
		for i := 0; i < n; i++ {
			items[i] = json.RawMessage(`{"id":"x","type":"investigation","attributes":{}}`)
		}
		body, _ := json.Marshal(map[string]any{
			"data": items,
			"meta": map[string]any{"page": map[string]any{"total": total}},
		})
		return driftUnwrapInvestigationsPage(body)
	}

	maxPages := driftInvestigationsMaxPages(total, size)
	items, gotTotal, err := paginateOffset(fetch, size, maxPages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTotal != total {
		t.Fatalf("total = %d, want %d", gotTotal, total)
	}
	if len(items) != total {
		t.Fatalf("len(items) = %d, want %d", len(items), total)
	}
	if calls != 3 { // 2,2,1
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDriftUnwrapInvestigationsPagePropagatesFetchError(t *testing.T) {
	fetchErr := errors.New("boom")
	fetch := func(offset, sz int) ([]json.RawMessage, int, error) {
		return nil, 0, fetchErr
	}
	_, _, err := paginateOffset(fetch, 10, 3)
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want %v", err, fetchErr)
	}
}
