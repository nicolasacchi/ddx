package commands

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRumlogsLogsMetricCreateBody(t *testing.T) {
	cases := []struct {
		name string
		in   rumlogsLogsMetricFields
		want map[string]any
	}{
		{
			name: "count, minimal",
			in: rumlogsLogsMetricFields{
				ID:              "logs.bot.count",
				AggregationType: "count",
			},
			want: map[string]any{
				"data": map[string]any{
					"id":   "logs.bot.count",
					"type": "logs_metrics",
					"attributes": map[string]any{
						"compute": map[string]any{"aggregation_type": "count"},
					},
				},
			},
		},
		{
			name: "distribution with path, percentiles, filter, group_by",
			in: rumlogsLogsMetricFields{
				ID:                    "logs.page.load.count",
				AggregationType:       "distribution",
				Path:                  "@duration",
				PathSet:               true,
				IncludePercentiles:    true,
				IncludePercentilesSet: true,
				Query:                 "service:web* AND @http.status_code:[200 TO 299]",
				QuerySet:              true,
				GroupBy: []map[string]any{
					{"path": "@http.status_code", "tag_name": "status_code"},
				},
			},
			want: map[string]any{
				"data": map[string]any{
					"id":   "logs.page.load.count",
					"type": "logs_metrics",
					"attributes": map[string]any{
						"compute": map[string]any{
							"aggregation_type":    "distribution",
							"path":                "@duration",
							"include_percentiles": true,
						},
						"filter": map[string]any{"query": "service:web* AND @http.status_code:[200 TO 299]"},
						"group_by": []map[string]any{
							{"path": "@http.status_code", "tag_name": "status_code"},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsLogsMetricCreateBody(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsArchiveBody(t *testing.T) {
	dest := json.RawMessage(`{"type":"s3","bucket":"my-bucket","integration":{"account_id":"123456789012","role_name":"my-role"}}`)

	cases := []struct {
		name string
		in   rumlogsArchiveFields
		want map[string]any
	}{
		{
			name: "minimal required fields",
			in: rumlogsArchiveFields{
				Name:        "Nginx Archive",
				Query:       "source:nginx",
				Destination: dest,
			},
			want: map[string]any{
				"data": map[string]any{
					"type": "archives",
					"attributes": map[string]any{
						"name":        "Nginx Archive",
						"query":       "source:nginx",
						"destination": dest,
					},
				},
			},
		},
		{
			name: "all optional fields set",
			in: rumlogsArchiveFields{
				Name:                    "Nginx Archive",
				Query:                   "source:nginx",
				Destination:             dest,
				CompressionMethod:       "GZIP",
				CompressionMethodSet:    true,
				IncludeTags:             true,
				IncludeTagsSet:          true,
				RehydrationMaxScanGB:    100,
				RehydrationMaxScanGBSet: true,
				RehydrationTags:         []string{"team:intake", "team:app"},
			},
			want: map[string]any{
				"data": map[string]any{
					"type": "archives",
					"attributes": map[string]any{
						"name":                            "Nginx Archive",
						"query":                           "source:nginx",
						"destination":                     dest,
						"compression_method":              "GZIP",
						"include_tags":                    true,
						"rehydration_max_scan_size_in_gb": int64(100),
						"rehydration_tags":                []string{"team:intake", "team:app"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsArchiveBody(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsParseDestinationJSON(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid JSON object", raw: `{"type":"s3","bucket":"b"}`, wantErr: false},
		{name: "empty is an error", raw: "", wantErr: true},
		{name: "invalid JSON is an error", raw: "not-json", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rumlogsParseDestinationJSON(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.raw {
				t.Fatalf("got %s, want %s", got, tc.raw)
			}
		})
	}
}

func TestRumlogsParseForwarderJSON(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid JSON object", raw: `{"type":"http","endpoint":"https://example.com"}`, wantErr: false},
		{name: "empty is an error", raw: "", wantErr: true},
		{name: "invalid JSON is an error", raw: "{not valid", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rumlogsParseForwarderJSON(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.raw {
				t.Fatalf("got %s, want %s", got, tc.raw)
			}
		})
	}
}

func TestRumlogsCustomDestAttrs(t *testing.T) {
	fwd := json.RawMessage(`{"type":"http","endpoint":"https://example.com"}`)

	cases := []struct {
		name string
		in   rumlogsCustomDestFields
		want map[string]any
	}{
		{
			name: "create: name and forwarder_destination only",
			in: rumlogsCustomDestFields{
				Name: "Nginx logs", NameSet: true,
				ForwarderDestination: fwd,
			},
			want: map[string]any{
				"name":                  "Nginx logs",
				"forwarder_destination": fwd,
			},
		},
		{
			name: "create: all fields set",
			in: rumlogsCustomDestFields{
				Name: "Nginx logs", NameSet: true,
				Query: "source:nginx", QuerySet: true,
				Enabled: true, EnabledSet: true,
				ForwardTags: true, ForwardTagsSet: true,
				ForwardTagsRestrictionList:        []string{"datacenter", "host"},
				ForwardTagsRestrictionListType:    "ALLOW_LIST",
				ForwardTagsRestrictionListTypeSet: true,
				ForwarderDestination:              fwd,
			},
			want: map[string]any{
				"name":                               "Nginx logs",
				"query":                              "source:nginx",
				"enabled":                            true,
				"forward_tags":                       true,
				"forward_tags_restriction_list":      []string{"datacenter", "host"},
				"forward_tags_restriction_list_type": "ALLOW_LIST",
				"forwarder_destination":              fwd,
			},
		},
		{
			name: "update: partial — only enabled changed",
			in: rumlogsCustomDestFields{
				Enabled: false, EnabledSet: true,
			},
			want: map[string]any{
				"enabled": false,
			},
		},
		{
			name: "update: nothing set yields empty attrs",
			in:   rumlogsCustomDestFields{},
			want: map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rumlogsCustomDestAttrs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRumlogsCustomDestCreateAndUpdateBody(t *testing.T) {
	fwd := json.RawMessage(`{"type":"http","endpoint":"https://example.com"}`)
	f := rumlogsCustomDestFields{
		Name: "Nginx logs", NameSet: true,
		ForwarderDestination: fwd,
	}

	gotCreate := rumlogsCustomDestCreateBody(f)
	wantCreate := map[string]any{
		"data": map[string]any{
			"type": "custom_destination",
			"attributes": map[string]any{
				"name":                  "Nginx logs",
				"forwarder_destination": fwd,
			},
		},
	}
	if !reflect.DeepEqual(gotCreate, wantCreate) {
		t.Fatalf("create: got %#v, want %#v", gotCreate, wantCreate)
	}

	gotUpdate := rumlogsCustomDestUpdateBody("00000000-0000-0000-0000-000000000001", f)
	wantUpdate := map[string]any{
		"data": map[string]any{
			"id":   "00000000-0000-0000-0000-000000000001",
			"type": "custom_destination",
			"attributes": map[string]any{
				"name":                  "Nginx logs",
				"forwarder_destination": fwd,
			},
		},
	}
	if !reflect.DeepEqual(gotUpdate, wantUpdate) {
		t.Fatalf("update: got %#v, want %#v", gotUpdate, wantUpdate)
	}
}
