package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestIrPersonaAPIValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"backend", "BACKEND"},
		{"BACKEND", "BACKEND"},
		{"frontend", "BROWSER"},
		{"FrontEnd", "BROWSER"},
		{"mobile", "MOBILE"},
		{"all", "ALL"},
		{" all ", "ALL"},
	}
	for _, c := range cases {
		got, err := irPersonaAPIValue(c.in)
		if err != nil {
			t.Fatalf("irPersonaAPIValue(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("irPersonaAPIValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIrPersonaAPIValueInvalid(t *testing.T) {
	if _, err := irPersonaAPIValue("desktop"); err == nil {
		t.Fatalf("expected error for invalid persona")
	}
}

func TestIrBuildIssueSearchBody(t *testing.T) {
	body := irBuildIssueSearchBody("service:orders-*", 1000, 2000, "BACKEND")
	data := body["data"].(map[string]any)
	if data["type"] != "search_request" {
		t.Fatalf("type = %v", data["type"])
	}
	attrs := data["attributes"].(map[string]any)
	if attrs["query"] != "service:orders-*" {
		t.Fatalf("query = %v", attrs["query"])
	}
	if attrs["from"] != int64(1000000) {
		t.Fatalf("from = %v, want 1000000 (ms)", attrs["from"])
	}
	if attrs["to"] != int64(2000000) {
		t.Fatalf("to = %v, want 2000000 (ms)", attrs["to"])
	}
	if attrs["persona"] != "BACKEND" {
		t.Fatalf("persona = %v", attrs["persona"])
	}
	// The schema doesn't document "sort" or "page" attributes — they must
	// not be sent.
	if _, ok := attrs["sort"]; ok {
		t.Fatalf("sort should not be sent, not part of IssuesSearchRequestDataAttributes")
	}
	if _, ok := attrs["page"]; ok {
		t.Fatalf("page should not be sent, not part of IssuesSearchRequestDataAttributes")
	}
}

func TestIrParseIssueItems(t *testing.T) {
	raw := json.RawMessage(`{
		"data": [
			{"id": "abc-123", "type": "error_tracking_search_result", "attributes": {"total_count": 82, "impacted_sessions": 12}}
		]
	}`)
	items, err := irParseIssueItems(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0]["id"] != "abc-123" {
		t.Fatalf("id = %v", items[0]["id"])
	}
	if items[0]["total_count"] != float64(82) {
		t.Fatalf("total_count = %v", items[0]["total_count"])
	}
}

func TestIrMergePersonaIssuesTagsAndSorts(t *testing.T) {
	perPersona := map[string][]map[string]any{
		"backend":  {{"id": "b1", "total_count": float64(10)}},
		"frontend": {{"id": "f1", "total_count": float64(50)}},
		"mobile":   {{"id": "m1", "total_count": float64(30)}},
	}

	merged := irMergePersonaIssues(perPersona)
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}

	// Sorted by total_count descending: f1 (50), m1 (30), b1 (10).
	if merged[0]["id"] != "f1" || merged[1]["id"] != "m1" || merged[2]["id"] != "b1" {
		t.Fatalf("merge order = %v, %v, %v", merged[0]["id"], merged[1]["id"], merged[2]["id"])
	}

	if merged[0]["persona"] != "frontend" {
		t.Fatalf("f1 persona tag = %v", merged[0]["persona"])
	}
	if merged[1]["persona"] != "mobile" {
		t.Fatalf("m1 persona tag = %v", merged[1]["persona"])
	}
	if merged[2]["persona"] != "backend" {
		t.Fatalf("b1 persona tag = %v", merged[2]["persona"])
	}
}

func TestIrMergePersonaIssuesStableTiebreak(t *testing.T) {
	// Equal total_count: fanout order (backend, frontend, mobile) must win,
	// and within a persona, original order must be preserved.
	perPersona := map[string][]map[string]any{
		"mobile":   {{"id": "m1", "total_count": float64(5)}},
		"backend":  {{"id": "b1", "total_count": float64(5)}, {"id": "b2", "total_count": float64(5)}},
		"frontend": {{"id": "f1", "total_count": float64(5)}},
	}

	merged := irMergePersonaIssues(perPersona)
	got := []string{}
	for _, m := range merged {
		got = append(got, m["id"].(string))
	}
	want := []string{"b1", "b2", "f1", "m1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merge order = %v, want %v", got, want)
		}
	}
}

func TestIrMergePersonaIssuesEmpty(t *testing.T) {
	merged := irMergePersonaIssues(map[string][]map[string]any{})
	if len(merged) != 0 {
		t.Fatalf("expected empty merge, got %v", merged)
	}
}

func TestIrSearchIssuesAllPersonasMergesAll(t *testing.T) {
	fetch := func(persona string) ([]map[string]any, error) {
		switch persona {
		case "backend":
			return []map[string]any{{"id": "b1", "total_count": float64(1)}}, nil
		case "frontend":
			return []map[string]any{{"id": "f1", "total_count": float64(2)}}, nil
		case "mobile":
			return []map[string]any{{"id": "m1", "total_count": float64(3)}}, nil
		default:
			return nil, fmt.Errorf("unexpected persona %s", persona)
		}
	}

	merged, err := irSearchIssuesAllPersonas(fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	// Highest total_count first: m1 (3), f1 (2), b1 (1).
	if merged[0]["id"] != "m1" || merged[1]["id"] != "f1" || merged[2]["id"] != "b1" {
		t.Fatalf("merge order = %v", merged)
	}
}

func TestIrSearchIssuesAllPersonasPartialFailure(t *testing.T) {
	boom := errors.New("boom")
	fetch := func(persona string) ([]map[string]any, error) {
		if persona == "mobile" {
			return nil, boom
		}
		return []map[string]any{{"id": persona, "total_count": float64(1)}}, nil
	}

	merged, err := irSearchIssuesAllPersonas(fetch)
	if err != nil {
		t.Fatalf("unexpected error when only one persona fails: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2 (mobile failed, backend+frontend survive)", len(merged))
	}
}

func TestIrSearchIssuesAllPersonasAllFail(t *testing.T) {
	boom := errors.New("boom")
	fetch := func(persona string) ([]map[string]any, error) {
		return nil, boom
	}

	_, err := irSearchIssuesAllPersonas(fetch)
	if err == nil {
		t.Fatalf("expected error when all personas fail")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
}

func TestIrTruncateMarker(t *testing.T) {
	cases := []struct {
		n, limit      int
		wantTruncated bool
		wantKeep      int
	}{
		{10, 50, false, 10},
		{50, 50, false, 50},
		{100, 50, true, 50},
		{10, 0, false, 10},
		{10, -1, false, 10},
	}
	for _, c := range cases {
		truncated, keep := irTruncateMarker(c.n, c.limit)
		if truncated != c.wantTruncated || keep != c.wantKeep {
			t.Fatalf("irTruncateMarker(%d, %d) = (%v, %d), want (%v, %d)", c.n, c.limit, truncated, keep, c.wantTruncated, c.wantKeep)
		}
	}
}

func TestIrNormalizeIssueState(t *testing.T) {
	for _, s := range []string{"OPEN", "ACKNOWLEDGED", "RESOLVED", "IGNORED", "EXCLUDED"} {
		got, err := irNormalizeIssueState(s)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", s, err)
		}
		if got != s {
			t.Fatalf("got = %s, want %s", got, s)
		}
	}
	got, err := irNormalizeIssueState("resolved")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "RESOLVED" {
		t.Fatalf("got = %s, want RESOLVED (case-insensitive)", got)
	}
}

func TestIrNormalizeIssueStateInvalid(t *testing.T) {
	if _, err := irNormalizeIssueState("CLOSED"); err == nil {
		t.Fatalf("expected error for invalid state")
	}
}

func TestIrBuildIssueStateBody(t *testing.T) {
	body := irBuildIssueStateBody("c1726a66-1f64-11ee-b338-da7ad0900002", "RESOLVED")
	data := body["data"].(map[string]any)
	if data["id"] != "c1726a66-1f64-11ee-b338-da7ad0900002" {
		t.Fatalf("id = %v", data["id"])
	}
	if data["type"] != "error_tracking_issue" {
		t.Fatalf("type = %v", data["type"])
	}
	attrs := data["attributes"].(map[string]any)
	if attrs["state"] != "RESOLVED" {
		t.Fatalf("state = %v", attrs["state"])
	}
}

func TestIrBuildIssueAssigneeBody(t *testing.T) {
	body := irBuildIssueAssigneeBody("87cb11a0-278c-440a-99fe-701223c80296")
	data := body["data"].(map[string]any)
	if data["id"] != "87cb11a0-278c-440a-99fe-701223c80296" {
		t.Fatalf("id = %v", data["id"])
	}
	if data["type"] != "assignee" {
		t.Fatalf("type = %v", data["type"])
	}
}

func TestIrIsEmail(t *testing.T) {
	if !irIsEmail("user@example.com") {
		t.Fatalf("expected user@example.com to be recognized as an email")
	}
	if irIsEmail("87cb11a0-278c-440a-99fe-701223c80296") {
		t.Fatalf("expected a UUID to not be recognized as an email")
	}
}
