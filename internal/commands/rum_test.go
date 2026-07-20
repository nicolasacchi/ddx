package commands

import (
	"reflect"
	"testing"
)

func TestRumlogsBuildAggregateBody(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		compute string
		groupBy string
		from    int64
		to      int64
		limit   int
		want    map[string]any
	}{
		{
			name:    "bare aggregation, no group-by",
			query:   "@type:error",
			compute: "count",
			groupBy: "",
			from:    1000,
			to:      2000,
			limit:   50,
			want: map[string]any{
				"filter": map[string]any{
					"query": "@type:error",
					"from":  "1000000",
					"to":    "2000000",
				},
				"compute": []map[string]any{
					{"aggregation": "count"},
				},
			},
		},
		{
			name:    "avg(@field) style with group-by",
			query:   "@type:view",
			compute: "avg(@view.loading_time)",
			groupBy: "@session.type",
			from:    100,
			to:      200,
			limit:   25,
			want: map[string]any{
				"filter": map[string]any{
					"query": "@type:view",
					"from":  "100000",
					"to":    "200000",
				},
				"compute": []map[string]any{
					{"aggregation": "avg", "metric": "@view.loading_time"},
				},
				"group_by": []map[string]any{
					{"facet": "@session.type", "limit": 25, "sort": map[string]any{"order": "desc"}},
				},
			},
		},
		{
			name:    "cardinality with group-by",
			query:   "*",
			compute: "cardinality(@usr.id)",
			groupBy: "@view.url",
			from:    1,
			to:      2,
			limit:   10,
			want: map[string]any{
				"filter": map[string]any{
					"query": "*",
					"from":  "1000",
					"to":    "2000",
				},
				"compute": []map[string]any{
					{"aggregation": "cardinality", "metric": "@usr.id"},
				},
				"group_by": []map[string]any{
					{"facet": "@view.url", "limit": 10, "sort": map[string]any{"order": "desc"}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsBuildAggregateBody(tc.query, tc.compute, tc.groupBy, tc.from, tc.to, tc.limit)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsRetentionFilterAttrs(t *testing.T) {
	cases := []struct {
		name string
		in   rumlogsRetentionFilterFields
		want map[string]any
	}{
		{
			name: "create: all required fields set, optional omitted",
			in: rumlogsRetentionFilterFields{
				Name: "Retention filter for session", NameSet: true,
				EventType: "session", EventTypeSet: true,
				SampleRate: 50.5, SampleRateSet: true,
			},
			want: map[string]any{
				"name":        "Retention filter for session",
				"event_type":  "session",
				"sample_rate": 50.5,
			},
		},
		{
			name: "create: query and enabled explicitly set",
			in: rumlogsRetentionFilterFields{
				Name: "f", NameSet: true,
				EventType: "view", EventTypeSet: true,
				Query: "@session.has_replay:true", QuerySet: true,
				SampleRate: 100, SampleRateSet: true,
				Enabled: false, EnabledSet: true,
			},
			want: map[string]any{
				"name":        "f",
				"event_type":  "view",
				"query":       "@session.has_replay:true",
				"sample_rate": 100.0,
				"enabled":     false,
			},
		},
		{
			name: "update: partial — only sample_rate changed",
			in: rumlogsRetentionFilterFields{
				SampleRate: 10, SampleRateSet: true,
			},
			want: map[string]any{
				"sample_rate": 10.0,
			},
		},
		{
			name: "update: nothing set yields empty attrs",
			in:   rumlogsRetentionFilterFields{},
			want: map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsRetentionFilterAttrs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsRetentionFilterCreateBody(t *testing.T) {
	f := rumlogsRetentionFilterFields{
		Name: "f", NameSet: true,
		EventType: "session", EventTypeSet: true,
		SampleRate: 50, SampleRateSet: true,
	}
	got := rumlogsRetentionFilterCreateBody(f)
	want := map[string]any{
		"data": map[string]any{
			"type": "retention_filters",
			"attributes": map[string]any{
				"name":        "f",
				"event_type":  "session",
				"sample_rate": 50.0,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRumlogsRetentionFilterUpdateBody(t *testing.T) {
	f := rumlogsRetentionFilterFields{
		Enabled: false, EnabledSet: true,
	}
	got := rumlogsRetentionFilterUpdateBody("051601eb-54a0-abc0-03f9-cc02efa18892", f)
	want := map[string]any{
		"data": map[string]any{
			"id":   "051601eb-54a0-abc0-03f9-cc02efa18892",
			"type": "retention_filters",
			"attributes": map[string]any{
				"enabled": false,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRumlogsRumMetricCreateBody(t *testing.T) {
	cases := []struct {
		name string
		in   rumlogsRumMetricFields
		want map[string]any
	}{
		{
			name: "count aggregation, minimal",
			in: rumlogsRumMetricFields{
				ID:              "rum.sessions.web.count",
				EventType:       "session",
				AggregationType: "count",
			},
			want: map[string]any{
				"data": map[string]any{
					"id":   "rum.sessions.web.count",
					"type": "rum_metrics",
					"attributes": map[string]any{
						"event_type": "session",
						"compute":    map[string]any{"aggregation_type": "count"},
					},
				},
			},
		},
		{
			name: "distribution with path, percentiles, filter, group_by, uniqueness",
			in: rumlogsRumMetricFields{
				ID:                    "rum.view.duration",
				EventType:             "view",
				AggregationType:       "distribution",
				Path:                  "@duration",
				PathSet:               true,
				IncludePercentiles:    true,
				IncludePercentilesSet: true,
				Query:                 "@service:web-api",
				QuerySet:              true,
				GroupBy: []map[string]any{
					{"path": "@browser.name", "tag_name": "browser_name"},
				},
				UniquenessWhen: "match",
				UniquenessSet:  true,
			},
			want: map[string]any{
				"data": map[string]any{
					"id":   "rum.view.duration",
					"type": "rum_metrics",
					"attributes": map[string]any{
						"event_type": "view",
						"compute": map[string]any{
							"aggregation_type":    "distribution",
							"path":                "@duration",
							"include_percentiles": true,
						},
						"filter": map[string]any{"query": "@service:web-api"},
						"group_by": []map[string]any{
							{"path": "@browser.name", "tag_name": "browser_name"},
						},
						"uniqueness": map[string]any{"when": "match"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsRumMetricCreateBody(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsParseCompute(t *testing.T) {
	cases := []struct {
		name        string
		spec        string
		wantAggType string
		wantPath    string
		wantErr     bool
	}{
		{name: "bare count", spec: "count", wantAggType: "count", wantPath: ""},
		{name: "distribution with path", spec: "distribution/@duration", wantAggType: "distribution", wantPath: "@duration"},
		{name: "path with nested slash", spec: "distribution/@http/status", wantAggType: "distribution", wantPath: "@http/status"},
		{name: "empty is an error", spec: "", wantErr: true},
		{name: "whitespace trimmed", spec: "  count  ", wantAggType: "count", wantPath: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aggType, path, err := rumlogsParseCompute(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if aggType != tc.wantAggType || path != tc.wantPath {
				t.Fatalf("got (%q, %q), want (%q, %q)", aggType, path, tc.wantAggType, tc.wantPath)
			}
		})
	}
}

func TestRumlogsParseGroupBy(t *testing.T) {
	cases := []struct {
		name  string
		specs []string
		want  []map[string]any
	}{
		{
			name:  "nil input",
			specs: nil,
			want:  nil,
		},
		{
			name:  "bare path only",
			specs: []string{"@browser.name"},
			want: []map[string]any{
				{"path": "@browser.name"},
			},
		},
		{
			name:  "path with tag_name",
			specs: []string{"@browser.name:browser_name"},
			want: []map[string]any{
				{"path": "@browser.name", "tag_name": "browser_name"},
			},
		},
		{
			name:  "multiple entries, blanks dropped",
			specs: []string{"@a", " ", "@b:tag_b", ""},
			want: []map[string]any{
				{"path": "@a"},
				{"path": "@b", "tag_name": "tag_b"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsParseGroupBy(tc.specs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
