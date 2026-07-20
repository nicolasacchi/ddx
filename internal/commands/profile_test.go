package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProfilerCheckEndpointFilterTag(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"empty query", "", false},
		{"unrelated tag", "kube_deployment:web-canary", false},
		{"endpoint-shaped substring but not the tag", "some_endpoint:foo", false},
		{"exact @endpoint tag", "@endpoint:ProductsController#show", true},
		{"@endpoint combined with other clauses", "service:web @endpoint:foo env:production", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := profilerCheckEndpointFilterTag(tc.query)
			if tc.wantErr && err == nil {
				t.Fatalf("profilerCheckEndpointFilterTag(%q) = nil, want error", tc.query)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("profilerCheckEndpointFilterTag(%q) = %v, want nil", tc.query, err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "@endpoint:") {
				t.Fatalf("error message should mention @endpoint:, got %q", err.Error())
			}
		})
	}
}

func TestProfilerHeapSamplesEndpointWarning(t *testing.T) {
	cases := []struct {
		name     string
		profType string
		by       string
		wantWarn bool
	}{
		{"heap-live-samples + endpoint warns", "heap-live-samples", "endpoint", true},
		{"heap-live-samples + function is fine", "heap-live-samples", "function", false},
		{"heap-live-size + endpoint does not warn here (handled post-hoc)", "heap-live-size", "endpoint", false},
		{"cpu-time + endpoint is fine", "cpu-time", "endpoint", false},
		{"heap-live-samples + summary is fine", "heap-live-samples", "summary", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := profilerHeapSamplesEndpointWarning(tc.profType, tc.by)
			if tc.wantWarn && msg == "" {
				t.Fatalf("profilerHeapSamplesEndpointWarning(%q, %q) = \"\", want a warning", tc.profType, tc.by)
			}
			if !tc.wantWarn && msg != "" {
				t.Fatalf("profilerHeapSamplesEndpointWarning(%q, %q) = %q, want \"\"", tc.profType, tc.by, msg)
			}
		})
	}
}

func TestProfilerDiffRepresentativenessWarning(t *testing.T) {
	cases := []struct {
		name                                                             string
		beforeAggregated, beforeInWindow, afterAggregated, afterInWindow int
		wantWarn                                                         bool
	}{
		{"balanced sides, no warning", 100, 500, 110, 520, false},
		{"before window empty", 0, 0, 50, 200, true},
		{"after window empty", 50, 200, 0, 0, true},
		{"exactly 5x is still fine (boundary)", 20, 200, 100, 500, false},
		{"just over 5x warns (after larger)", 19, 200, 100, 500, true},
		{"just over 5x warns (before larger)", 100, 500, 19, 200, true},
		{"aggregated zero but window non-zero warns", 0, 10, 50, 200, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := profilerDiffRepresentativenessWarning(tc.beforeAggregated, tc.beforeInWindow, tc.afterAggregated, tc.afterInWindow)
			if tc.wantWarn && msg == "" {
				t.Fatalf("expected a warning for before=(%d,%d) after=(%d,%d), got none",
					tc.beforeAggregated, tc.beforeInWindow, tc.afterAggregated, tc.afterInWindow)
			}
			if !tc.wantWarn && msg != "" {
				t.Fatalf("expected no warning for before=(%d,%d) after=(%d,%d), got %q",
					tc.beforeAggregated, tc.beforeInWindow, tc.afterAggregated, tc.afterInWindow, msg)
			}
		})
	}
}

func TestProfilerFunctionIdentityKey(t *testing.T) {
	cases := []struct {
		name       string
		fnA, fileA string
		fnB, fileB string
		wantEqual  bool
	}{
		{"identical function+file match", "foo", "a.rb", "foo", "a.rb", true},
		{"different file does not match", "foo", "a.rb", "foo", "b.rb", false},
		{"different function does not match", "foo", "a.rb", "bar", "a.rb", false},
		{"no accidental collision across the function/file boundary", "fo", "oa.rb", "foo", "a.rb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ka := profilerFunctionIdentityKey(tc.fnA, tc.fileA)
			kb := profilerFunctionIdentityKey(tc.fnB, tc.fileB)
			if (ka == kb) != tc.wantEqual {
				t.Fatalf("profilerFunctionIdentityKey(%q,%q)==profilerFunctionIdentityKey(%q,%q) = %v, want %v",
					tc.fnA, tc.fileA, tc.fnB, tc.fileB, ka == kb, tc.wantEqual)
			}
		})
	}
}

// buildTestAggregateRaw assembles a minimal raw aggregate-endpoint response
// with a packed flame graph, for exercising profilerExtractFunctionTotals
// without a network call.
//
// Tree: root(frame 0, non-leaf)
//
//	├── leaf frame 1: func_a/file_a.rb, value 100
//	├── leaf frame 2: func_a/file_a.rb, value 50   (same identity, different frame index)
//	└── leaf frame 3: func_b/file_b.rb, value 30
func buildTestAggregateRaw(t *testing.T) json.RawMessage {
	t.Helper()
	flameGraph := []any{
		0, 0.0, 0.0, []any{
			[]any{1, 100.0, 0.0, []any{}},
			[]any{2, 50.0, 0.0, []any{}},
			[]any{3, 30.0, 0.0, []any{}},
		},
	}
	flameGraphBytes, err := json.Marshal(flameGraph)
	if err != nil {
		t.Fatalf("marshal test flame graph: %v", err)
	}

	resp := struct {
		FlameGraph  json.RawMessage `json:"flameGraph"`
		Frames      [][]int         `json:"frames"`
		Strings     []string        `json:"strings"`
		FrameSchema []string        `json:"frameSchema"`
	}{
		FlameGraph: flameGraphBytes,
		// frame 0: root/"", frame 1 & 2: func_a/file_a.rb (duplicate identity),
		// frame 3: func_b/file_b.rb.
		Frames:      [][]int{{0, 1}, {2, 3}, {2, 3}, {4, 5}},
		Strings:     []string{"root", "", "func_a", "file_a.rb", "func_b", "file_b.rb"},
		FrameSchema: []string{"function", "file"},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal test aggregate response: %v", err)
	}
	return raw
}

func TestProfilerExtractFunctionTotals(t *testing.T) {
	raw := buildTestAggregateRaw(t)

	entries, err := profilerExtractFunctionTotals(raw)
	if err != nil {
		t.Fatalf("profilerExtractFunctionTotals: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 merged entries (func_a, func_b), got %d: %#v", len(entries), entries)
	}

	byKey := make(map[string]profilerFunctionEntry, len(entries))
	for _, e := range entries {
		byKey[profilerFunctionIdentityKey(e.Function, e.File)] = e
	}

	funcA, ok := byKey[profilerFunctionIdentityKey("func_a", "file_a.rb")]
	if !ok {
		t.Fatalf("missing func_a/file_a.rb entry in %#v", entries)
	}
	if funcA.Value != 150 {
		t.Errorf("func_a value = %v, want 150 (100+50 merged across frame indices 1 and 2)", funcA.Value)
	}

	funcB, ok := byKey[profilerFunctionIdentityKey("func_b", "file_b.rb")]
	if !ok {
		t.Fatalf("missing func_b/file_b.rb entry in %#v", entries)
	}
	if funcB.Value != 30 {
		t.Errorf("func_b value = %v, want 30", funcB.Value)
	}
}

func TestProfilerExtractFunctionTotalsErrors(t *testing.T) {
	t.Run("missing flame graph", func(t *testing.T) {
		raw := []byte(`{"frames":[],"strings":[],"frameSchema":[]}`)
		if _, err := profilerExtractFunctionTotals(raw); err == nil {
			t.Fatal("expected error for response with no flameGraph")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		raw := []byte(`not json`)
		if _, err := profilerExtractFunctionTotals(raw); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

func TestProfilerExtractWindowMeta(t *testing.T) {
	raw := []byte(`{"numberOfProfiles":42,"totalProfilesCount":100,"metadata":{"service":"web"}}`)
	meta, err := profilerExtractWindowMeta(raw)
	if err != nil {
		t.Fatalf("profilerExtractWindowMeta: %v", err)
	}
	if meta.ProfilesAggregated != 42 {
		t.Errorf("ProfilesAggregated = %d, want 42", meta.ProfilesAggregated)
	}
	if meta.ProfilesInWindow != 100 {
		t.Errorf("ProfilesInWindow = %d, want 100", meta.ProfilesInWindow)
	}
	if len(meta.Metadata) == 0 {
		t.Error("expected non-empty Metadata to be preserved")
	}
}

func TestProfilerBuildFunctionDiff(t *testing.T) {
	before := []profilerFunctionEntry{
		{Function: "grew", File: "a.rb", Value: 100},
		{Function: "shrank", File: "b.rb", Value: 200},
		{Function: "only_before", File: "c.rb", Value: 50},
	}
	after := []profilerFunctionEntry{
		{Function: "grew", File: "a.rb", Value: 300},
		{Function: "shrank", File: "b.rb", Value: 150},
		{Function: "only_after", File: "d.rb", Value: 40},
	}

	out := profilerBuildFunctionDiff(before, after, "v1", "v2", "cpu-time", 0, json.RawMessage(`{}`), json.RawMessage(`{}`))

	rowsRaw, ok := out["top_by_abs_delta"].([]profilerFunctionDiffRow)
	if !ok {
		t.Fatalf("top_by_abs_delta has unexpected type %T", out["top_by_abs_delta"])
	}
	if len(rowsRaw) != 4 {
		t.Fatalf("expected 4 rows (union of both sides), got %d: %#v", len(rowsRaw), rowsRaw)
	}

	byFn := make(map[string]profilerFunctionDiffRow, len(rowsRaw))
	for _, r := range rowsRaw {
		byFn[r.Function] = r
	}

	grew := byFn["grew"]
	if grew.Before != 100 || grew.After != 300 || grew.Delta != 200 {
		t.Errorf("grew row = %+v, want before=100 after=300 delta=200", grew)
	}
	if grew.PercentChg != 200.0 {
		t.Errorf("grew percent_change = %v, want 200 (100 -> 300)", grew.PercentChg)
	}

	onlyBefore := byFn["only_before"]
	if onlyBefore.Before != 50 || onlyBefore.After != 0 || onlyBefore.Delta != -50 {
		t.Errorf("only_before row = %+v, want before=50 after=0 delta=-50", onlyBefore)
	}

	onlyAfter := byFn["only_after"]
	if onlyAfter.Before != 0 || onlyAfter.After != 40 || onlyAfter.Delta != 40 {
		t.Errorf("only_after row = %+v, want before=0 after=40 delta=40", onlyAfter)
	}
	if onlyAfter.PercentChg != 0 {
		t.Errorf("only_after percent_change = %v, want 0 (can't compute from a zero base)", onlyAfter.PercentChg)
	}

	// Sorted by absolute delta descending: grew(200) > shrank(50 abs) > only_before(50 abs) > only_after(40).
	if rowsRaw[0].Function != "grew" {
		t.Errorf("expected largest abs-delta row first (grew), got %q", rowsRaw[0].Function)
	}
	if absF(rowsRaw[0].Delta) < absF(rowsRaw[len(rowsRaw)-1].Delta) {
		t.Errorf("rows are not sorted by descending absolute delta: %#v", rowsRaw)
	}

	if out["before_functions"] != len(before) {
		t.Errorf("before_functions = %v, want %d", out["before_functions"], len(before))
	}
	if out["after_functions"] != len(after) {
		t.Errorf("after_functions = %v, want %d", out["after_functions"], len(after))
	}
}

func TestProfilerBuildFunctionDiffTopN(t *testing.T) {
	before := []profilerFunctionEntry{
		{Function: "a", File: "f.rb", Value: 0},
		{Function: "b", File: "f.rb", Value: 0},
		{Function: "c", File: "f.rb", Value: 0},
	}
	after := []profilerFunctionEntry{
		{Function: "a", File: "f.rb", Value: 10},
		{Function: "b", File: "f.rb", Value: 50},
		{Function: "c", File: "f.rb", Value: 5},
	}

	out := profilerBuildFunctionDiff(before, after, "v1", "v2", "cpu-time", 1, nil, nil)
	rows, ok := out["top_by_abs_delta"].([]profilerFunctionDiffRow)
	if !ok {
		t.Fatalf("top_by_abs_delta has unexpected type %T", out["top_by_abs_delta"])
	}
	if len(rows) != 1 {
		t.Fatalf("expected --top 1 to truncate to 1 row, got %d", len(rows))
	}
	if rows[0].Function != "b" {
		t.Errorf("expected the largest-delta function (b, delta=50) to survive truncation, got %q", rows[0].Function)
	}
}
