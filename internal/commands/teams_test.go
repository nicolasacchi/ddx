package commands

import (
	"encoding/json"
	"testing"
)

func TestDriftFindTeamIDByHandle(t *testing.T) {
	data := json.RawMessage(`[
		{"id": "00000000-0000-0000-0000-000000000001", "attributes": {"handle": "platform-team", "name": "Platform"}},
		{"id": "00000000-0000-0000-0000-000000000002", "attributes": {"handle": "checkout-team", "name": "Checkout"}}
	]`)

	t.Run("exact match", func(t *testing.T) {
		id, ok := driftFindTeamIDByHandle(data, "checkout-team")
		if !ok {
			t.Fatal("expected a match")
		}
		if id != "00000000-0000-0000-0000-000000000002" {
			t.Fatalf("id = %q, want checkout-team's id", id)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, ok := driftFindTeamIDByHandle(data, "nonexistent-team")
		if ok {
			t.Fatal("expected no match")
		}
	})

	t.Run("malformed input", func(t *testing.T) {
		_, ok := driftFindTeamIDByHandle(json.RawMessage(`not json`), "x")
		if ok {
			t.Fatal("expected no match for malformed JSON")
		}
	})
}

func TestDriftFlattenSingleV2Item(t *testing.T) {
	t.Run("merges attributes and id", func(t *testing.T) {
		raw := json.RawMessage(`{"id": "00000000-0000-0000-0000-000000000003", "type": "team", "attributes": {"handle": "example-team", "name": "Example Team"}}`)
		out := driftFlattenSingleV2Item(raw)

		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("output not valid JSON: %v", err)
		}
		if got["id"] != "00000000-0000-0000-0000-000000000003" {
			t.Fatalf("id = %v", got["id"])
		}
		if got["handle"] != "example-team" || got["name"] != "Example Team" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("passthrough on unrecognized shape", func(t *testing.T) {
		raw := json.RawMessage(`{"foo": "bar"}`)
		out := driftFlattenSingleV2Item(raw)
		if string(out) != string(raw) {
			t.Fatalf("expected passthrough, got %s", out)
		}
	})
}

func TestDriftMergeTeamMemberships(t *testing.T) {
	t.Run("merges memberships list into team object", func(t *testing.T) {
		team := json.RawMessage(`{"id": "00000000-0000-0000-0000-000000000003", "handle": "example-team", "name": "Example Team"}`)
		memberships := json.RawMessage(`{
			"data": [
				{"id": "TeamMembership-1", "type": "team_memberships", "attributes": {"role": "admin"}}
			]
		}`)

		out := driftMergeTeamMemberships(team, memberships)

		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("output not valid JSON: %v", err)
		}
		if got["handle"] != "example-team" {
			t.Fatalf("expected original team fields preserved, got %+v", got)
		}
		memList, ok := got["memberships"].([]any)
		if !ok || len(memList) != 1 {
			t.Fatalf("memberships = %+v", got["memberships"])
		}
		first := memList[0].(map[string]any)
		if first["id"] != "TeamMembership-1" || first["role"] != "admin" {
			t.Fatalf("first membership = %+v", first)
		}
	})

	t.Run("malformed team object passes through unchanged", func(t *testing.T) {
		team := json.RawMessage(`not json`)
		out := driftMergeTeamMemberships(team, json.RawMessage(`{"data":[]}`))
		if string(out) != string(team) {
			t.Fatalf("expected passthrough of malformed team, got %s", out)
		}
	})
}
