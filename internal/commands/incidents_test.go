package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIrBuildIncidentCreateBodyMinimal(t *testing.T) {
	body, err := irBuildIncidentCreateBody("Checkout errors spiking", "", "", false, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := body["data"].(map[string]any)
	if data["type"] != "incidents" {
		t.Fatalf("type = %v, want incidents", data["type"])
	}
	attrs := data["attributes"].(map[string]any)
	if attrs["title"] != "Checkout errors spiking" {
		t.Fatalf("title = %v", attrs["title"])
	}
	if attrs["customer_impacted"] != false {
		t.Fatalf("customer_impacted = %v, want false", attrs["customer_impacted"])
	}
	if _, ok := attrs["customer_impact_scope"]; ok {
		t.Fatalf("customer_impact_scope should be absent when not impacted")
	}
	if _, ok := attrs["fields"]; ok {
		t.Fatalf("fields should be absent when no severity/summary given")
	}
	if _, ok := data["relationships"]; ok {
		t.Fatalf("relationships should be absent when no commander given")
	}

	// Rule 8: never set fields.slug or public_id ourselves.
	raw, _ := json.Marshal(body)
	s := string(raw)
	if strings.Contains(s, `"slug"`) || strings.Contains(s, `"public_id"`) {
		t.Fatalf("create body must never set slug/public_id, got: %s", s)
	}
}

func TestIrBuildIncidentCreateBodyFull(t *testing.T) {
	body, err := irBuildIncidentCreateBody(
		"Elevated 500s",
		"SEV-2",
		"Investigating",
		true,
		"EU checkout down for ~10 min",
		"00000000-0000-0000-0000-000000000000",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := body["data"].(map[string]any)
	attrs := data["attributes"].(map[string]any)

	if attrs["customer_impact_scope"] != "EU checkout down for ~10 min" {
		t.Fatalf("customer_impact_scope = %v", attrs["customer_impact_scope"])
	}

	fields := attrs["fields"].(map[string]any)
	sev := fields["severity"].(map[string]any)
	if sev["type"] != "dropdown" || sev["value"] != "SEV-2" {
		t.Fatalf("severity field = %v", sev)
	}
	summary := fields["summary"].(map[string]any)
	if summary["type"] != "textbox" || summary["value"] != "Investigating" {
		t.Fatalf("summary field = %v", summary)
	}

	rel := data["relationships"].(map[string]any)
	commander := rel["commander_user"].(map[string]any)["data"].(map[string]any)
	if commander["id"] != "00000000-0000-0000-0000-000000000000" || commander["type"] != "users" {
		t.Fatalf("commander relationship = %v", commander)
	}
}

func TestIrBuildIncidentCreateBodyRequiresTitle(t *testing.T) {
	if _, err := irBuildIncidentCreateBody("", "", "", false, "", ""); err == nil {
		t.Fatalf("expected error for empty title")
	}
	if _, err := irBuildIncidentCreateBody("   ", "", "", false, "", ""); err == nil {
		t.Fatalf("expected error for whitespace-only title")
	}
}

func TestIrBuildIncidentCreateBodyRequiresImpactScopeWhenImpacted(t *testing.T) {
	if _, err := irBuildIncidentCreateBody("Title", "", "", true, "", ""); err == nil {
		t.Fatalf("expected error when customer_impacted=true and no impact scope")
	}
	if _, err := irBuildIncidentCreateBody("Title", "", "", true, "  ", ""); err == nil {
		t.Fatalf("expected error when customer_impacted=true and whitespace-only impact scope")
	}
}

func TestIrBuildIncidentCreateBodyValidatesSeverity(t *testing.T) {
	if _, err := irBuildIncidentCreateBody("Title", "SEV-9", "", false, "", ""); err == nil {
		t.Fatalf("expected error for invalid severity")
	}
	for _, sev := range []string{"SEV-1", "SEV-2", "SEV-3", "SEV-4", "SEV-5"} {
		if _, err := irBuildIncidentCreateBody("Title", sev, "", false, "", ""); err != nil {
			t.Fatalf("unexpected error for valid severity %s: %v", sev, err)
		}
	}
}

func TestIrExtractIncidentIdentity(t *testing.T) {
	raw := json.RawMessage(`{
		"data": {
			"id": "00000000-0000-0000-1234-000000000000",
			"type": "incidents",
			"attributes": {
				"title": "A test incident title",
				"public_id": "45",
				"slug": "IR-45"
			}
		}
	}`)

	uuid, publicID, slug, title := irExtractIncidentIdentity(raw)
	if uuid != "00000000-0000-0000-1234-000000000000" {
		t.Fatalf("uuid = %q", uuid)
	}
	if publicID != "45" {
		t.Fatalf("publicID = %q", publicID)
	}
	if slug != "IR-45" {
		t.Fatalf("slug = %q", slug)
	}
	if title != "A test incident title" {
		t.Fatalf("title = %q", title)
	}
}

func TestIrExtractIncidentIdentityMissingFields(t *testing.T) {
	raw := json.RawMessage(`{"data": {"id": "abc", "type": "incidents", "attributes": {"title": "t"}}}`)
	uuid, publicID, slug, title := irExtractIncidentIdentity(raw)
	if uuid != "abc" || title != "t" {
		t.Fatalf("uuid/title = %q/%q", uuid, title)
	}
	if publicID != "" || slug != "" {
		t.Fatalf("expected empty publicID/slug when API doesn't return them, got %q/%q", publicID, slug)
	}
}

func TestIrExtractIncidentIdentityMalformed(t *testing.T) {
	uuid, publicID, slug, title := irExtractIncidentIdentity(json.RawMessage(`not json`))
	if uuid != "" || publicID != "" || slug != "" || title != "" {
		t.Fatalf("expected all-empty on malformed input")
	}
}

func TestIrParseIncidentSearchPage(t *testing.T) {
	raw := json.RawMessage(`{
		"data": {
			"type": "incidents_search_results",
			"attributes": {
				"total": 2,
				"incidents": [
					{"data": {"id": "1", "type": "incidents", "attributes": {"title": "one"}}},
					{"data": {"id": "2", "type": "incidents", "attributes": {"title": "two"}}}
				],
				"facets": {}
			}
		}
	}`)

	items, total, err := irParseIncidentSearchPage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestIrParseIncidentSearchPageMalformed(t *testing.T) {
	if _, _, err := irParseIncidentSearchPage(json.RawMessage(`not json`)); err == nil {
		t.Fatalf("expected error on malformed input")
	}
}

func TestIrUnwrapIncidentItemNested(t *testing.T) {
	item := json.RawMessage(`{"data": {"id": "1", "type": "incidents", "attributes": {"title": "one"}}}`)
	got := irUnwrapIncidentItem(item)

	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("unmarshal unwrapped item: %v", err)
	}
	if obj.ID != "1" {
		t.Fatalf("id = %q, want 1", obj.ID)
	}
}

func TestIrUnwrapIncidentItemFlat(t *testing.T) {
	// Defensive fallback: if the API ever returns the flatter shape (no
	// nested "data" key), the item itself should pass through unchanged.
	item := json.RawMessage(`{"id": "1", "type": "incidents", "attributes": {"title": "one"}}`)
	got := irUnwrapIncidentItem(item)

	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if obj.ID != "1" {
		t.Fatalf("id = %q, want 1", obj.ID)
	}
}

func TestIrFlattenIncidentItems(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"data": {"id": "1", "type": "incidents", "attributes": {"title": "one"}}}`),
		json.RawMessage(`{"data": {"id": "2", "type": "incidents", "attributes": {"title": "two"}}}`),
	}

	out := irFlattenIncidentItems(items)

	var result []map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal flattened output: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	if result[0]["id"] != "1" || result[0]["title"] != "one" {
		t.Fatalf("result[0] = %v", result[0])
	}
	if result[1]["id"] != "2" || result[1]["title"] != "two" {
		t.Fatalf("result[1] = %v", result[1])
	}
}

func TestIrFlattenIncidentItemsEmpty(t *testing.T) {
	out := irFlattenIncidentItems(nil)
	var result []map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
}
