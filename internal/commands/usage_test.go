package commands

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"
)

func TestUcParseMonth(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty passes through", in: "", want: ""},
		{name: "bare YYYY-MM", in: "2026-06", want: "2026-06"},
		{name: "full RFC3339", in: "2026-06-15T00:00:00Z", want: "2026-06"},
		{name: "RFC3339 with offset", in: "2026-01-31T23:59:59+02:00", want: "2026-01"},
		{name: "single-digit month rejected", in: "2026-6", wantErr: true},
		{name: "garbage rejected", in: "not-a-month", wantErr: true},
		{name: "day-precision rejected", in: "2026-06-15", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ucParseMonth(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ucParseMonth(%q) = %q, nil; want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ucParseMonth(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ucParseMonth(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUsagecostHourISO(t *testing.T) {
	// 2024-01-01T12:34:56Z
	unix := time.Date(2024, 1, 1, 12, 34, 56, 0, time.UTC).Unix()
	got := usagecostHourISO(unix)
	want := "2024-01-01T12"
	if got != want {
		t.Fatalf("usagecostHourISO(%d) = %q, want %q", unix, got, want)
	}
}

func TestUsagecostBuildHourlyParams(t *testing.T) {
	t.Run("no cursor", func(t *testing.T) {
		params := usagecostBuildHourlyParams("logs,apm", "2026-07-01T00", "2026-07-02T00", "")
		want := url.Values{
			"filter[timestamp][start]": []string{"2026-07-01T00"},
			"filter[timestamp][end]":   []string{"2026-07-02T00"},
			"filter[product_families]": []string{"logs,apm"},
		}
		for k, v := range want {
			if got := params.Get(k); got != v[0] {
				t.Fatalf("params[%q] = %q, want %q", k, got, v[0])
			}
		}
		if params.Get("page[next_record_id]") != "" {
			t.Fatalf("page[next_record_id] should be absent when cursor is empty")
		}
	})

	t.Run("with cursor", func(t *testing.T) {
		params := usagecostBuildHourlyParams("all", "2026-07-01T00", "2026-07-02T00", "cursor-123")
		if got := params.Get("page[next_record_id]"); got != "cursor-123" {
			t.Fatalf("page[next_record_id] = %q, want %q", got, "cursor-123")
		}
	})
}

func TestUsagecostExtractCursorPage(t *testing.T) {
	t.Run("array data with next cursor", func(t *testing.T) {
		raw := json.RawMessage(`{
			"data": [
				{"id": "a1", "type": "usage_timeseries", "attributes": {"product_family": "logs"}},
				{"id": "a2", "type": "usage_timeseries", "attributes": {"product_family": "apm"}}
			],
			"meta": {"pagination": {"next_record_id": "cursor-next"}}
		}`)

		items, next, err := usagecostExtractCursorPage(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if next != "cursor-next" {
			t.Fatalf("next = %q, want %q", next, "cursor-next")
		}
		if len(items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(items))
		}
		var first map[string]any
		if err := json.Unmarshal(items[0], &first); err != nil {
			t.Fatalf("item 0 not valid JSON: %v", err)
		}
		if first["id"] != "a1" {
			t.Fatalf("item 0 id = %v, want a1", first["id"])
		}
		if first["product_family"] != "logs" {
			t.Fatalf("item 0 attributes not flattened: %v", first)
		}
	})

	t.Run("null next_record_id means exhausted", func(t *testing.T) {
		raw := json.RawMessage(`{"data": [], "meta": {"pagination": {"next_record_id": null}}}`)
		items, next, err := usagecostExtractCursorPage(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if next != "" {
			t.Fatalf("next = %q, want empty", next)
		}
		if len(items) != 0 {
			t.Fatalf("len(items) = %d, want 0", len(items))
		}
	})

	t.Run("missing pagination meta entirely", func(t *testing.T) {
		raw := json.RawMessage(`{"data": [{"id": "x", "attributes": {"a": 1}}]}`)
		items, next, err := usagecostExtractCursorPage(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if next != "" {
			t.Fatalf("next = %q, want empty", next)
		}
		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		_, _, err := usagecostExtractCursorPage(json.RawMessage(`not json`))
		if err == nil {
			t.Fatalf("expected error for malformed JSON")
		}
	})
}

func TestUsagecostPaginateCursor(t *testing.T) {
	t.Run("stops when cursor empty after first page", func(t *testing.T) {
		calls := 0
		sleeps := 0
		fetch := func(cursor string) ([]json.RawMessage, string, error) {
			calls++
			if cursor != "" {
				t.Fatalf("expected empty cursor on first call, got %q", cursor)
			}
			return []json.RawMessage{json.RawMessage(`{"id":1}`)}, "", nil
		}

		items, err := usagecostPaginateCursor(fetch, 10, func(time.Duration) { sleeps++ })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}
		if sleeps != 0 {
			t.Fatalf("sleeps = %d, want 0 (never sleep after the last page)", sleeps)
		}
	})

	t.Run("follows cursor across pages and sleeps between them", func(t *testing.T) {
		pages := map[string][]json.RawMessage{
			"":   {json.RawMessage(`{"id":1}`)},
			"c2": {json.RawMessage(`{"id":2}`)},
			"c3": {json.RawMessage(`{"id":3}`)},
		}
		next := map[string]string{"": "c2", "c2": "c3", "c3": ""}
		calls := 0
		sleeps := 0

		fetch := func(cursor string) ([]json.RawMessage, string, error) {
			calls++
			return pages[cursor], next[cursor], nil
		}

		items, err := usagecostPaginateCursor(fetch, 10, func(time.Duration) { sleeps++ })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 3 {
			t.Fatalf("calls = %d, want 3", calls)
		}
		if len(items) != 3 {
			t.Fatalf("len(items) = %d, want 3", len(items))
		}
		if sleeps != 2 {
			t.Fatalf("sleeps = %d, want 2 (between pages 1-2 and 2-3, never after the last)", sleeps)
		}
	})

	t.Run("maxPages caps a runaway cursor", func(t *testing.T) {
		calls := 0
		fetch := func(cursor string) ([]json.RawMessage, string, error) {
			calls++
			// Always report a new cursor — without the cap this loops forever.
			return []json.RawMessage{json.RawMessage(`{"id":1}`)}, "next-" + cursor, nil
		}

		items, err := usagecostPaginateCursor(fetch, 4, func(time.Duration) {})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 4 {
			t.Fatalf("calls = %d, want 4 (maxPages cap)", calls)
		}
		if len(items) != 4 {
			t.Fatalf("len(items) = %d, want 4", len(items))
		}
	})

	t.Run("repeated cursor stops the loop", func(t *testing.T) {
		calls := 0
		fetch := func(cursor string) ([]json.RawMessage, string, error) {
			calls++
			// Misbehaving API: keeps returning the same cursor it was given.
			return []json.RawMessage{json.RawMessage(`{"id":1}`)}, "same", nil
		}

		items, err := usagecostPaginateCursor(fetch, 10, func(time.Duration) {})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 2 {
			t.Fatalf("calls = %d, want 2 (page 1 cursor='' -> next='same'; page 2 cursor='same' -> next='same' stops)", calls)
		}
		if len(items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(items))
		}
	})

	t.Run("fetch error preserves items accumulated so far", func(t *testing.T) {
		calls := 0
		fetchErr := errors.New("boom: 429 from upstream")
		fetch := func(cursor string) ([]json.RawMessage, string, error) {
			calls++
			if calls == 1 {
				return []json.RawMessage{json.RawMessage(`{"id":1}`)}, "c2", nil
			}
			return nil, "", fetchErr
		}

		items, err := usagecostPaginateCursor(fetch, 10, func(time.Duration) {})
		if !errors.Is(err, fetchErr) {
			t.Fatalf("err = %v, want %v", err, fetchErr)
		}
		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1 (first page retained)", len(items))
		}
	})
}

func TestUsagecostUnwrapUsage(t *testing.T) {
	t.Run("unwraps usage array", func(t *testing.T) {
		raw := json.RawMessage(`{"usage": [{"hour": "2024-01-01T00:00:00Z"}]}`)
		got := usagecostUnwrapUsage(raw)
		var items []json.RawMessage
		if err := json.Unmarshal(got, &items); err != nil {
			t.Fatalf("expected array, got unmarshal error: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(items))
		}
	})

	t.Run("passthrough when no usage key", func(t *testing.T) {
		raw := json.RawMessage(`{"other": [1, 2, 3]}`)
		got := usagecostUnwrapUsage(raw)
		if string(got) != string(raw) {
			t.Fatalf("got %s, want passthrough %s", got, raw)
		}
	})
}

func TestUsagecostFlattenV2Single(t *testing.T) {
	t.Run("merges id into attributes", func(t *testing.T) {
		raw := json.RawMessage(`{"id": "abc-123", "type": "billing_dimensions", "attributes": {"month": "2024-01-01T00:00:00Z", "values": ["infra_host"]}}`)
		got := usagecostFlattenV2Single(raw)
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("result not valid JSON: %v", err)
		}
		if obj["id"] != "abc-123" {
			t.Fatalf("id = %v, want abc-123", obj["id"])
		}
		if obj["month"] != "2024-01-01T00:00:00Z" {
			t.Fatalf("attributes not merged: %v", obj)
		}
	})

	t.Run("passthrough without attributes", func(t *testing.T) {
		raw := json.RawMessage(`{"id": "abc-123"}`)
		got := usagecostFlattenV2Single(raw)
		if string(got) != string(raw) {
			t.Fatalf("got %s, want passthrough %s", got, raw)
		}
	})
}

func TestUsagecostCountItems(t *testing.T) {
	cases := []struct {
		name string
		in   json.RawMessage
		want int
	}{
		{name: "array of 3", in: json.RawMessage(`[1,2,3]`), want: 3},
		{name: "empty array", in: json.RawMessage(`[]`), want: 0},
		{name: "object is not an array", in: json.RawMessage(`{"a":1}`), want: 0},
		{name: "malformed", in: json.RawMessage(`not json`), want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usagecostCountItems(tc.in); got != tc.want {
				t.Fatalf("usagecostCountItems(%s) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
