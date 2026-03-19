package commands

import "encoding/json"

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
