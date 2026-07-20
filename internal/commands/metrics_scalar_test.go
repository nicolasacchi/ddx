package commands

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTracesscalarValidateReducer(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty is allowed (default applies upstream)", "", false},
		{"avg", "avg", false},
		{"last", "last", false},
		{"max", "max", false},
		{"min", "min", false},
		{"sum", "sum", false},
		{"percentile is not in the CLI surface", "percentile", true},
		{"garbage", "p95", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tracesscalarValidateReducer(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("tracesscalarValidateReducer(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestTracesscalarBuildQueries(t *testing.T) {
	got := tracesscalarBuildQueries([]string{"avg:system.cpu.user{*} by {env}", "sum:trace.hits{*}"}, "sum")
	want := []map[string]any{
		{"name": "query0", "data_source": "metrics", "aggregator": "sum", "query": "avg:system.cpu.user{*} by {env}"},
		{"name": "query1", "data_source": "metrics", "aggregator": "sum", "query": "sum:trace.hits{*}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracesscalarBuildQueries() = %#v, want %#v", got, want)
	}
}

func TestTracesscalarBuildFormulas(t *testing.T) {
	if got := tracesscalarBuildFormulas(nil); got != nil {
		t.Fatalf("tracesscalarBuildFormulas(nil) = %#v, want nil", got)
	}
	got := tracesscalarBuildFormulas([]string{"query1 / query0"})
	want := []map[string]any{{"formula": "query1 / query0"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracesscalarBuildFormulas() = %#v, want %#v", got, want)
	}
}

func TestTracesscalarBuildRequestBody(t *testing.T) {
	body := tracesscalarBuildRequestBody(
		[]string{"avg:system.cpu.user{*} by {env}"},
		nil,
		"avg",
		1568899800000,
		1568923200000,
	)

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("body[data] not a map: %#v", body)
	}
	if data["type"] != "scalar_request" {
		t.Fatalf("data.type = %v, want scalar_request", data["type"])
	}
	attrs, ok := data["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("data.attributes not a map: %#v", data)
	}
	if attrs["from"] != int64(1568899800000) {
		t.Fatalf("attrs.from = %v, want 1568899800000", attrs["from"])
	}
	if attrs["to"] != int64(1568923200000) {
		t.Fatalf("attrs.to = %v, want 1568923200000", attrs["to"])
	}
	if _, hasFormulas := attrs["formulas"]; hasFormulas {
		t.Fatalf("attrs.formulas present with no formulas requested: %#v", attrs)
	}
	queries, ok := attrs["queries"].([]map[string]any)
	if !ok || len(queries) != 1 {
		t.Fatalf("attrs.queries = %#v, want one query", attrs["queries"])
	}
	if queries[0]["aggregator"] != "avg" {
		t.Fatalf("queries[0].aggregator = %v, want avg", queries[0]["aggregator"])
	}

	// With formulas, the key must be present.
	withFormulas := tracesscalarBuildRequestBody([]string{"avg:a{*}"}, []string{"query0 * 2"}, "avg", 0, 1000)
	attrs2 := withFormulas["data"].(map[string]any)["attributes"].(map[string]any)
	if _, hasFormulas := attrs2["formulas"]; !hasFormulas {
		t.Fatalf("attrs.formulas missing when formulas were requested: %#v", attrs2)
	}
}

// canned response fixture — matches the /api/v2/query/scalar response shape
// from the OpenAPI spec / research brief: one group column ("env") and one
// data column ("a"), values running in parallel by row index.
const scalarResponseFixture = `{
  "data": {
    "type": "scalar_response",
    "attributes": {
      "columns": [
        {"type": "group", "name": "env", "values": [["production"], ["staging"]]},
        {"type": "number", "name": "a", "values": [0.42, 0.18], "meta": {"unit": [{"name": "percent"}]}}
      ]
    }
  },
  "errors": null
}`

func TestTracesscalarFlattenScalarResponse(t *testing.T) {
	t.Run("group + single data column", func(t *testing.T) {
		rows, err := tracesscalarFlattenScalarResponse(json.RawMessage(scalarResponseFixture))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []map[string]any{
			{"name": "a", "tags": map[string]string{"env": "production"}, "value": 0.42},
			{"name": "a", "tags": map[string]string{"env": "staging"}, "value": 0.18},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %#v, want %#v", rows, want)
		}
	})

	t.Run("no group columns — scalar aggregate with no by clause", func(t *testing.T) {
		raw := `{"data":{"type":"scalar_response","attributes":{"columns":[
			{"type":"number","name":"a","values":[42.5]}
		]}}}`
		rows, err := tracesscalarFlattenScalarResponse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []map[string]any{
			{"name": "a", "value": 42.5},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %#v, want %#v", rows, want)
		}
	})

	t.Run("multiple data columns (formula outputs) share the same group rows", func(t *testing.T) {
		raw := `{"data":{"type":"scalar_response","attributes":{"columns":[
			{"type":"group","name":"service","values":[["web"],["api"]]},
			{"type":"number","name":"a","values":[10,20]},
			{"type":"number","name":"b","values":[1,2]}
		]}}}`
		rows, err := tracesscalarFlattenScalarResponse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []map[string]any{
			{"name": "a", "tags": map[string]string{"service": "web"}, "value": 10.0},
			{"name": "a", "tags": map[string]string{"service": "api"}, "value": 20.0},
			{"name": "b", "tags": map[string]string{"service": "web"}, "value": 1.0},
			{"name": "b", "tags": map[string]string{"service": "api"}, "value": 2.0},
		}
		if !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %#v, want %#v", rows, want)
		}
	})

	t.Run("nullable value in a number column comes through as nil", func(t *testing.T) {
		raw := `{"data":{"type":"scalar_response","attributes":{"columns":[
			{"type":"group","name":"env","values":[["prod"],["staging"]]},
			{"type":"number","name":"a","values":[1.5,null]}
		]}}}`
		rows, err := tracesscalarFlattenScalarResponse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rows[1]["value"] != nil {
			t.Fatalf("rows[1].value = %#v, want nil", rows[1]["value"])
		}
	})

	t.Run("no columns at all yields an empty (non-nil) slice", func(t *testing.T) {
		raw := `{"data":{"type":"scalar_response","attributes":{"columns":[]}}}`
		rows, err := tracesscalarFlattenScalarResponse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rows == nil || len(rows) != 0 {
			t.Fatalf("rows = %#v, want empty non-nil slice", rows)
		}
	})

	t.Run("malformed JSON errors", func(t *testing.T) {
		_, err := tracesscalarFlattenScalarResponse(json.RawMessage(`not json`))
		if err == nil {
			t.Fatalf("expected error for malformed JSON")
		}
	})
}
