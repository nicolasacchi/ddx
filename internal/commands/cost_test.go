package commands

import (
	"reflect"
	"testing"
)

func TestUsagecostBuildAttributionParams(t *testing.T) {
	t.Run("defaults fields to * and omits optional params", func(t *testing.T) {
		params := usagecostBuildAttributionParams("2026-06", "2026-06", "", "", "", "", "")
		if got := params.Get("start_month"); got != "2026-06" {
			t.Fatalf("start_month = %q, want 2026-06", got)
		}
		if got := params.Get("end_month"); got != "2026-06" {
			t.Fatalf("end_month = %q, want 2026-06", got)
		}
		if got := params.Get("fields"); got != "*" {
			t.Fatalf("fields = %q, want * (default)", got)
		}
		for _, key := range []string{"tag_breakdown_keys", "sort_direction", "sort_name", "next_record_id"} {
			if params.Has(key) {
				t.Fatalf("params should omit %q when not requested, got %q", key, params.Get(key))
			}
		}
	})

	t.Run("explicit fields override the default", func(t *testing.T) {
		params := usagecostBuildAttributionParams("2026-01", "2026-06", "infra_host_on_demand_cost,infra_host_percentage_in_account", "", "", "", "")
		want := "infra_host_on_demand_cost,infra_host_percentage_in_account"
		if got := params.Get("fields"); got != want {
			t.Fatalf("fields = %q, want %q", got, want)
		}
	})

	t.Run("tag_breakdown_keys trims whitespace and drops empties", func(t *testing.T) {
		params := usagecostBuildAttributionParams("2026-06", "2026-06", "*", " team , env ,,region", "", "", "")
		want := "team,env,region"
		if got := params.Get("tag_breakdown_keys"); got != want {
			t.Fatalf("tag_breakdown_keys = %q, want %q", got, want)
		}
	})

	t.Run("blank tags produce no tag_breakdown_keys param", func(t *testing.T) {
		params := usagecostBuildAttributionParams("2026-06", "2026-06", "*", " , , ", "", "", "")
		if params.Has("tag_breakdown_keys") {
			t.Fatalf("tag_breakdown_keys should be absent for all-blank input, got %q", params.Get("tag_breakdown_keys"))
		}
	})

	t.Run("sort and cursor pass through when set", func(t *testing.T) {
		params := usagecostBuildAttributionParams("2026-06", "2026-06", "*", "", "desc", "infra_host", "cursor-abc")
		if got := params.Get("sort_direction"); got != "desc" {
			t.Fatalf("sort_direction = %q, want desc", got)
		}
		if got := params.Get("sort_name"); got != "infra_host" {
			t.Fatalf("sort_name = %q, want infra_host", got)
		}
		if got := params.Get("next_record_id"); got != "cursor-abc" {
			t.Fatalf("next_record_id = %q, want cursor-abc", got)
		}
	})
}

func TestUsagecostBuildAttributionParamsFullExample(t *testing.T) {
	// Guards the exact set of keys emitted for a fully-specified call — a
	// stray/missing key here silently breaks the Datadog request.
	params := usagecostBuildAttributionParams("2026-01", "2026-03", "infra_host_total_cost", "team,env", "asc", "infra_host", "cursor-1")

	got := map[string]string{}
	for k := range params {
		got[k] = params.Get(k)
	}
	want := map[string]string{
		"start_month":        "2026-01",
		"end_month":          "2026-03",
		"fields":             "infra_host_total_cost",
		"tag_breakdown_keys": "team,env",
		"sort_direction":     "asc",
		"sort_name":          "infra_host",
		"next_record_id":     "cursor-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("params = %#v, want %#v", got, want)
	}
}
