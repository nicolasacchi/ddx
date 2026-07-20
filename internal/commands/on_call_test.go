package commands

import (
	"testing"
)

func TestDriftBuildOnCallPageBody(t *testing.T) {
	t.Run("valid team handle target", func(t *testing.T) {
		body, err := driftBuildOnCallPageBody("", "my-team", "", "DB replica lag", "", "high", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data := body["data"].(map[string]any)
		if data["type"] != "pages" {
			t.Fatalf("type = %v, want pages", data["type"])
		}
		attrs := data["attributes"].(map[string]any)
		target := attrs["target"].(map[string]any)
		if target["identifier"] != "my-team" || target["type"] != "team_handle" {
			t.Fatalf("target = %+v, want identifier=my-team type=team_handle", target)
		}
		if attrs["title"] != "DB replica lag" || attrs["urgency"] != "high" {
			t.Fatalf("attrs = %+v", attrs)
		}
		if _, ok := attrs["description"]; ok {
			t.Fatalf("description should be omitted when empty, got %+v", attrs)
		}
		if _, ok := attrs["tags"]; ok {
			t.Fatalf("tags should be omitted when empty, got %+v", attrs)
		}
	})

	t.Run("valid team id target with description and tags", func(t *testing.T) {
		body, err := driftBuildOnCallPageBody("00000000-0000-0000-0000-000000000001", "", "", "Title", "Some context", "low", "service:web, env:prod")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
		target := attrs["target"].(map[string]any)
		if target["type"] != "team_id" || target["identifier"] != "00000000-0000-0000-0000-000000000001" {
			t.Fatalf("target = %+v", target)
		}
		if attrs["description"] != "Some context" {
			t.Fatalf("description = %v", attrs["description"])
		}
		tags, ok := attrs["tags"].([]string)
		if !ok || len(tags) != 2 || tags[0] != "service:web" || tags[1] != "env:prod" {
			t.Fatalf("tags = %+v", attrs["tags"])
		}
	})

	t.Run("valid user id target", func(t *testing.T) {
		body, err := driftBuildOnCallPageBody("", "", "00000000-0000-0000-0000-000000000002", "Title", "", "high", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		target := body["data"].(map[string]any)["attributes"].(map[string]any)["target"].(map[string]any)
		if target["type"] != "user_id" {
			t.Fatalf("target = %+v", target)
		}
	})

	t.Run("no target is an error", func(t *testing.T) {
		_, err := driftBuildOnCallPageBody("", "", "", "Title", "", "high", "")
		if err == nil {
			t.Fatal("expected an error when no target is set")
		}
	})

	t.Run("multiple targets is an error", func(t *testing.T) {
		_, err := driftBuildOnCallPageBody("team-uuid", "team-handle", "", "Title", "", "high", "")
		if err == nil {
			t.Fatal("expected an error when multiple targets are set")
		}
	})

	t.Run("all three targets is an error", func(t *testing.T) {
		_, err := driftBuildOnCallPageBody("team-uuid", "team-handle", "user-uuid", "Title", "", "high", "")
		if err == nil {
			t.Fatal("expected an error when all three targets are set")
		}
	})

	t.Run("missing title is an error", func(t *testing.T) {
		_, err := driftBuildOnCallPageBody("", "my-team", "", "", "", "high", "")
		if err == nil {
			t.Fatal("expected an error when title is empty")
		}
	})

	t.Run("invalid urgency is an error", func(t *testing.T) {
		_, err := driftBuildOnCallPageBody("", "my-team", "", "Title", "", "critical", "")
		if err == nil {
			t.Fatal("expected an error for an urgency outside low/high")
		}
	})
}
