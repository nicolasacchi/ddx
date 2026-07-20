package commands

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/nicolasacchi/ddx/internal/client"
)

func TestCardinalityBuildEstimateParams(t *testing.T) {
	cases := []struct {
		name            string
		groups          string
		hoursAgo        int
		numAggregations int
		timespanH       int
		pct             bool
		want            url.Values
	}{
		{
			name: "all zero values omitted",
			want: url.Values{},
		},
		{
			name:   "groups only",
			groups: "app,host",
			want:   url.Values{"filter[groups]": {"app,host"}},
		},
		{
			name:            "every filter set",
			groups:          "app",
			hoursAgo:        49,
			numAggregations: 1,
			timespanH:       6,
			pct:             true,
			want: url.Values{
				"filter[groups]":           {"app"},
				"filter[hours_ago]":        {"49"},
				"filter[num_aggregations]": {"1"},
				"filter[timespan_h]":       {"6"},
				"filter[pct]":              {"true"},
			},
		},
		{
			name: "pct false omitted",
			pct:  false,
			want: url.Values{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cardinalityBuildEstimateParams(tc.groups, tc.hoursAgo, tc.numAggregations, tc.timespanH, tc.pct)
			if got.Encode() != tc.want.Encode() {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCardinalityValidateMetricType(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"gauge valid", "gauge", false},
		{"count valid", "count", false},
		{"rate valid", "rate", false},
		{"distribution valid", "distribution", false},
		{"empty invalid", "", true},
		{"unknown invalid", "histogram", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cardinalityValidateMetricType(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("cardinalityValidateMetricType(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestCardinalityBuildTagConfigBody(t *testing.T) {
	metricType := "distribution"
	tags := []string{"app", "datacenter"}
	trueVal := true
	falseVal := false

	cases := []struct {
		name               string
		metricName         string
		metricType         *string
		tags               *[]string
		excludeTagsMode    *bool
		includePercentiles *bool
		wantAttrs          map[string]any
	}{
		{
			name:       "create with all fields",
			metricName: "http.endpoint.request",
			metricType: &metricType,
			tags:       &tags,
			wantAttrs: map[string]any{
				"metric_type": "distribution",
				"tags":        []any{"app", "datacenter"},
			},
		},
		{
			name:       "update omits metric_type",
			metricName: "http.endpoint.request",
			metricType: nil,
			tags:       &tags,
			wantAttrs: map[string]any{
				"tags": []any{"app", "datacenter"},
			},
		},
		{
			name:               "explicit false is included, not omitted",
			metricName:         "http.endpoint.request",
			tags:               &tags,
			excludeTagsMode:    &falseVal,
			includePercentiles: &trueVal,
			wantAttrs: map[string]any{
				"tags":                []any{"app", "datacenter"},
				"exclude_tags_mode":   false,
				"include_percentiles": true,
			},
		},
		{
			name:       "nothing set beyond id/type",
			metricName: "http.endpoint.request",
			wantAttrs:  map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := cardinalityBuildTagConfigBody(tc.metricName, tc.metricType, tc.tags, tc.excludeTagsMode, tc.includePercentiles)
			data, ok := body["data"].(map[string]any)
			if !ok {
				t.Fatalf("body[data] not a map: %#v", body)
			}
			if data["type"] != "manage_tags" {
				t.Fatalf("type = %v, want manage_tags", data["type"])
			}
			if data["id"] != tc.metricName {
				t.Fatalf("id = %v, want %v", data["id"], tc.metricName)
			}
			gotAttrs, _ := json.Marshal(data["attributes"])
			wantAttrs, _ := json.Marshal(tc.wantAttrs)
			if string(gotAttrs) != string(wantAttrs) {
				t.Fatalf("attributes = %s, want %s", gotAttrs, wantAttrs)
			}
		})
	}
}

func TestCardinalityBuildTagIndexingRuleBody(t *testing.T) {
	name := "my-rule"
	matches := []string{"dd.test.*"}
	ignored := []string{"dd.test.excluded.*"}
	tags := []string{"env", "service"}
	trueVal := true
	order := 2

	cases := []struct {
		name              string
		ruleName          *string
		metricNameMatches *[]string
		ignoredMatches    *[]string
		tags              *[]string
		excludeTagsMode   *bool
		ruleOrder         *int
		wantAttrs         map[string]any
	}{
		{
			name:              "create shape",
			ruleName:          &name,
			metricNameMatches: &matches,
			tags:              &tags,
			wantAttrs: map[string]any{
				"name":                "my-rule",
				"metric_name_matches": []any{"dd.test.*"},
				"tags":                []any{"env", "service"},
			},
		},
		{
			name:      "update partial: only rule_order",
			ruleOrder: &order,
			wantAttrs: map[string]any{
				"rule_order": float64(2),
			},
		},
		{
			name:              "update all optional fields",
			ruleName:          &name,
			metricNameMatches: &matches,
			ignoredMatches:    &ignored,
			tags:              &tags,
			excludeTagsMode:   &trueVal,
			ruleOrder:         &order,
			wantAttrs: map[string]any{
				"name":                        "my-rule",
				"metric_name_matches":         []any{"dd.test.*"},
				"ignored_metric_name_matches": []any{"dd.test.excluded.*"},
				"tags":                        []any{"env", "service"},
				"exclude_tags_mode":           true,
				"rule_order":                  float64(2),
			},
		},
		{
			name:      "nothing set",
			wantAttrs: map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := cardinalityBuildTagIndexingRuleBody(tc.ruleName, tc.metricNameMatches, tc.ignoredMatches, tc.tags, tc.excludeTagsMode, tc.ruleOrder)
			data, ok := body["data"].(map[string]any)
			if !ok {
				t.Fatalf("body[data] not a map: %#v", body)
			}
			if data["type"] != "tag_indexing_rules" {
				t.Fatalf("type = %v, want tag_indexing_rules", data["type"])
			}
			if _, hasID := data["id"]; hasID {
				t.Fatalf("body must not carry an id field — the UUID belongs in the URL path")
			}

			// Round-trip through JSON so numeric types line up (float64) the
			// same way they would after a real json.Marshal/Unmarshal.
			raw, err := json.Marshal(data["attributes"])
			if err != nil {
				t.Fatalf("marshal attributes: %v", err)
			}
			var gotAttrs map[string]any
			if err := json.Unmarshal(raw, &gotAttrs); err != nil {
				t.Fatalf("unmarshal attributes: %v", err)
			}
			gotJSON, _ := json.Marshal(gotAttrs)
			wantJSON, _ := json.Marshal(tc.wantAttrs)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("attributes = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestCardinalityBuildReorderBody(t *testing.T) {
	ids := []string{"id-2", "id-1"}
	body := cardinalityBuildReorderBody(ids)

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("body[data] not a map: %#v", body)
	}
	if data["type"] != "tag_indexing_rules" {
		t.Fatalf("type = %v, want tag_indexing_rules", data["type"])
	}
	attrs, ok := data["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes not a map: %#v", data["attributes"])
	}
	gotIDs, ok := attrs["rule_ids"].([]string)
	if !ok {
		t.Fatalf("rule_ids not a []string: %#v", attrs["rule_ids"])
	}
	if len(gotIDs) != 2 || gotIDs[0] != "id-2" || gotIDs[1] != "id-1" {
		t.Fatalf("rule_ids = %v, want [id-2 id-1] (order preserved)", gotIDs)
	}
}

func TestCardinalityIsConflict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"409 conflict", &client.APIError{StatusCode: 409}, true},
		{"404 not found", &client.APIError{StatusCode: 404}, false},
		{"write_locked guard error", &client.APIError{Kind: "write_locked"}, false},
		{"nil error", nil, false},
		{"non-APIError", errNotAPIError{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cardinalityIsConflict(tc.err); got != tc.want {
				t.Fatalf("cardinalityIsConflict(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type errNotAPIError struct{}

func (errNotAPIError) Error() string { return "not an APIError" }
