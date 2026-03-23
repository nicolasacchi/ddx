package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// flattenV2Items takes a v2 API data array of {"id":"...", "attributes":{...}}
// and merges id into attributes for a cleaner output.
func flattenV2Items(data json.RawMessage) json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return data
	}

	var result []map[string]any
	for _, item := range items {
		var obj struct {
			ID         string          `json:"id"`
			Type       string          `json:"type"`
			Attributes json.RawMessage `json:"attributes"`
		}
		if json.Unmarshal(item, &obj) != nil {
			continue
		}
		if obj.Attributes == nil {
			// Not a v2 format — return original
			return data
		}
		var attrs map[string]any
		if json.Unmarshal(obj.Attributes, &attrs) != nil {
			continue
		}
		attrs["id"] = obj.ID
		result = append(result, attrs)
	}

	if result == nil {
		return data
	}
	out, _ := json.Marshal(result)
	return out
}

// extractWithMeta unwraps {"data": [...], "meta": {...}} and prints total count to stderr.
func extractWithMeta(raw json.RawMessage, cmd string) json.RawMessage {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			Page struct {
				TotalCount    int `json:"total_count"`
				TotalFiltered int `json:"total_filtered_count"`
			} `json:"page"`
		} `json:"meta"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Data != nil {
		total := wrapper.Meta.Page.TotalCount
		if total == 0 {
			total = wrapper.Meta.Page.TotalFiltered
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "%s: %d total results\n", cmd, total)
		}
		return wrapper.Data
	}
	return raw
}

// truncateArray limits a JSON array to n items.
func truncateArray(data json.RawMessage, n int) json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil || len(items) <= n {
		return data
	}
	out, _ := json.Marshal(items[:n])
	return out
}

// buildExplorerURL constructs a deep link to the Datadog UI.
func buildExplorerURL(explorer, query string, from, to int64) string {
	site := os.Getenv("DD_SITE")
	if site == "" {
		site = "datadoghq.eu"
	}
	base := "https://app." + site

	q := url.QueryEscape(query)
	fromMs := fmt.Sprintf("%d", from*1000)
	toMs := fmt.Sprintf("%d", to*1000)

	switch explorer {
	case "logs":
		return base + "/logs?query=" + q + "&from_ts=" + fromMs + "&to_ts=" + toMs
	case "rum":
		return base + "/rum/explorer?query=" + q + "&from_ts=" + fromMs + "&to_ts=" + toMs
	case "traces":
		return base + "/apm/traces?query=" + q + "&start=" + fromMs + "&end=" + toMs
	default:
		return base
	}
}
