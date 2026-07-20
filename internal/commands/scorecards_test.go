package commands

import (
	"encoding/json"
	"testing"
)

func TestDriftMergeRuleScorecards(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []map[string]any
	}{
		{
			name: "rule with matching included scorecard",
			raw: `{
				"data": [
					{
						"id": "rule-1",
						"type": "rule",
						"attributes": {"name": "Test Rule 1", "enabled": true},
						"relationships": {"scorecard": {"data": {"id": "scorecard-1", "type": "scorecard"}}}
					}
				],
				"included": [
					{"id": "scorecard-1", "type": "scorecard", "attributes": {"name": "Test Scorecard", "description": "Scorecard Description"}}
				]
			}`,
			want: []map[string]any{
				{
					"id":      "rule-1",
					"name":    "Test Rule 1",
					"enabled": true,
					"scorecard": map[string]any{
						"name":        "Test Scorecard",
						"description": "Scorecard Description",
					},
				},
			},
		},
		{
			name: "rule with no relationship (no include requested)",
			raw: `{
				"data": [
					{"id": "rule-2", "type": "rule", "attributes": {"name": "Bare Rule"}}
				]
			}`,
			want: []map[string]any{
				{"id": "rule-2", "name": "Bare Rule"},
			},
		},
		{
			name: "rule references a scorecard not present in included",
			raw: `{
				"data": [
					{
						"id": "rule-3",
						"type": "rule",
						"attributes": {"name": "Orphan Rule"},
						"relationships": {"scorecard": {"data": {"id": "missing-scorecard", "type": "scorecard"}}}
					}
				],
				"included": []
			}`,
			want: []map[string]any{
				{"id": "rule-3", "name": "Orphan Rule"},
			},
		},
		{
			name: "empty data array",
			raw:  `{"data": []}`,
			want: []map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := driftMergeRuleScorecards(json.RawMessage(tt.raw))

			var got []map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("output not a JSON array: %v (%s)", err, out)
			}

			wantJSON, _ := json.Marshal(tt.want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatalf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestDriftMergeRuleScorecardsFallback(t *testing.T) {
	// Not a v2 {"data":[...]} shape at all -> falls back to
	// flattenV2Items(extractData(raw)), i.e. the raw bytes pass through
	// unchanged since it's not a "data" array.
	raw := json.RawMessage(`{"unexpected": "shape"}`)
	out := driftMergeRuleScorecards(raw)
	if string(out) != string(raw) {
		t.Fatalf("expected passthrough of unrecognized shape, got %s", out)
	}
}
