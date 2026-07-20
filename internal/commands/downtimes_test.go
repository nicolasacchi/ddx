package commands

import (
	"testing"
)

func TestIrBuildDowntimeCreateBodyRequiresScope(t *testing.T) {
	if _, err := irBuildDowntimeCreateBody("", false, 0, "", "", "", ""); err == nil {
		t.Fatalf("expected error for empty scope")
	}
	if _, err := irBuildDowntimeCreateBody("   ", false, 0, "", "", "", ""); err == nil {
		t.Fatalf("expected error for whitespace-only scope")
	}
}

func TestIrBuildDowntimeCreateBodyMonitorIDAndTagsMutuallyExclusive(t *testing.T) {
	_, err := irBuildDowntimeCreateBody("env:prod", true, 123, "service:checkout", "", "", "")
	if err == nil {
		t.Fatalf("expected error when both --monitor-id and --monitor-tags are given")
	}
}

func TestIrBuildDowntimeCreateBodyMonitorID(t *testing.T) {
	body, err := irBuildDowntimeCreateBody("env:prod", true, 123, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	if attrs["scope"] != "env:prod" {
		t.Fatalf("scope = %v", attrs["scope"])
	}
	ident := attrs["monitor_identifier"].(map[string]any)
	if ident["monitor_id"] != int64(123) {
		t.Fatalf("monitor_id = %v", ident["monitor_id"])
	}
	if _, ok := ident["monitor_tags"]; ok {
		t.Fatalf("monitor_tags should be absent when monitor_id is set")
	}
}

func TestIrBuildDowntimeCreateBodyMonitorTags(t *testing.T) {
	body, err := irBuildDowntimeCreateBody("env:prod", false, 0, "service:checkout, team:frontend", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	ident := attrs["monitor_identifier"].(map[string]any)
	tags := ident["monitor_tags"].([]string)
	if len(tags) != 2 || tags[0] != "service:checkout" || tags[1] != "team:frontend" {
		t.Fatalf("monitor_tags = %v", tags)
	}
}

func TestIrBuildDowntimeCreateBodyDefaultsToMuteAllMonitors(t *testing.T) {
	// Per spec, monitor_identifier is required; when the caller gives
	// neither --monitor-id nor --monitor-tags, we must still send something.
	// The spec's own example for "mute all monitors in scope" is
	// monitor_tags: ["*"].
	body, err := irBuildDowntimeCreateBody("env:prod", false, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	ident := attrs["monitor_identifier"].(map[string]any)
	tags := ident["monitor_tags"].([]string)
	if len(tags) != 1 || tags[0] != "*" {
		t.Fatalf("monitor_tags = %v, want [\"*\"]", tags)
	}
}

func TestIrBuildDowntimeCreateBodyMessageOptional(t *testing.T) {
	body, err := irBuildDowntimeCreateBody("env:prod", false, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	if _, ok := attrs["message"]; ok {
		t.Fatalf("message should be absent when not given")
	}

	body, err = irBuildDowntimeCreateBody("env:prod", false, 0, "", "", "", "planned maintenance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs = body["data"].(map[string]any)["attributes"].(map[string]any)
	if attrs["message"] != "planned maintenance" {
		t.Fatalf("message = %v", attrs["message"])
	}
}

func TestIrBuildDowntimeCreateBodySchedule(t *testing.T) {
	// No start/end at all: schedule omitted entirely — per spec this means
	// "starts immediately, never ends".
	body, err := irBuildDowntimeCreateBody("env:prod", false, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs := body["data"].(map[string]any)["attributes"].(map[string]any)
	if _, ok := attrs["schedule"]; ok {
		t.Fatalf("schedule should be absent when neither start nor end given")
	}

	// Only start given (end optional per spec, contrary to the research
	// brief — the YAML wins).
	body, err = irBuildDowntimeCreateBody("env:prod", false, 0, "", "2026-07-20T09:00:00Z", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs = body["data"].(map[string]any)["attributes"].(map[string]any)
	sched := attrs["schedule"].(map[string]any)
	if sched["start"] != "2026-07-20T09:00:00Z" {
		t.Fatalf("schedule.start = %v", sched["start"])
	}
	if _, ok := sched["end"]; ok {
		t.Fatalf("schedule.end should be absent when not given")
	}

	// Both given.
	body, err = irBuildDowntimeCreateBody("env:prod", false, 0, "", "2026-07-20T09:00:00Z", "2026-07-20T11:00:00Z", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	attrs = body["data"].(map[string]any)["attributes"].(map[string]any)
	sched = attrs["schedule"].(map[string]any)
	if sched["start"] != "2026-07-20T09:00:00Z" || sched["end"] != "2026-07-20T11:00:00Z" {
		t.Fatalf("schedule = %v", sched)
	}
}

func TestIrBuildDowntimeCreateBodyType(t *testing.T) {
	body, err := irBuildDowntimeCreateBody("env:prod", false, 0, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := body["data"].(map[string]any)
	if data["type"] != "downtime" {
		t.Fatalf("type = %v, want downtime", data["type"])
	}
}

func TestIrBuildDowntimeCreateBodyEmptyMonitorTagsList(t *testing.T) {
	// A --monitor-tags value that's present but splits to nothing (e.g. just
	// commas/whitespace) shouldn't silently become the "mute all" default —
	// it's a user error worth surfacing.
	if _, err := irBuildDowntimeCreateBody("env:prod", false, 0, " , ,  ", "", "", ""); err == nil {
		t.Fatalf("expected error for --monitor-tags that splits to no tags")
	}
}

func TestIrFormatDowntimeBoundaryEmpty(t *testing.T) {
	got, err := irFormatDowntimeBoundary("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty", got)
	}
}

func TestIrFormatDowntimeBoundaryRFC3339(t *testing.T) {
	got, err := irFormatDowntimeBoundary("2026-07-20T09:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026-07-20T09:00:00Z" {
		t.Fatalf("got = %q", got)
	}
}

func TestIrFormatDowntimeBoundaryInvalid(t *testing.T) {
	if _, err := irFormatDowntimeBoundary("not-a-time"); err == nil {
		t.Fatalf("expected error for unparseable boundary")
	}
}
