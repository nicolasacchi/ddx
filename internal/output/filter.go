package output

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

// ApplyFilter applies a gjson path expression to JSON data.
func ApplyFilter(data json.RawMessage, path string) (json.RawMessage, error) {
	if path == "" {
		return data, nil
	}

	result := gjson.GetBytes(data, path)
	if !result.Exists() {
		return json.RawMessage("null"), nil
	}

	raw := result.Raw
	if raw == "" {
		return nil, fmt.Errorf("gjson filter %q returned non-JSON result", path)
	}
	return json.RawMessage(raw), nil
}
