package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/tidwall/gjson"
)

// helpersCheatSheetOnce ensures the gjson usage cheat sheet is printed at
// most once per process, even if several --jq filters miss across a single
// invocation (e.g. one per row of a table-driven caller).
var helpersCheatSheetOnce sync.Once

// helpersFilterCheatSheet is a 5-line reminder that --jq speaks gjson, not
// jq, with the handful of path forms that cover most real filters. Printed to
// stderr, never stdout, so it never pollutes JSON/table output.
const helpersFilterCheatSheet = `note: --jq uses gjson path syntax, not jq
  #.field              -> all values of a field across array items
  #.{a:a,b:b}          -> projection into a new object shape
  #(x=="y").id         -> query: element matching x=="y", extract id
  data.0.attributes     -> index by position; docs: https://gjson.dev
`

// ApplyFilter applies a gjson path expression to JSON data.
func ApplyFilter(data json.RawMessage, path string) (json.RawMessage, error) {
	if path == "" {
		return data, nil
	}

	result := gjson.GetBytes(data, path)

	// Only hint when the filter came back empty against non-empty input —
	// never when it legitimately returned data (including a legitimate
	// empty array/object result for a query that matched nothing on
	// purpose... that case is covered by helpersHasContent below, which
	// treats "[]"/"{}"/"null" input as having nothing to usefully filter).
	if helpersIsEmptyFilterResult(result) && helpersHasContent(data) {
		helpersCheatSheetOnce.Do(func() {
			fmt.Fprint(os.Stderr, helpersFilterCheatSheet)
		})
	}

	if !result.Exists() {
		return json.RawMessage("null"), nil
	}

	raw := result.Raw
	if raw == "" {
		return nil, fmt.Errorf("gjson filter %q returned non-JSON result", path)
	}
	return json.RawMessage(raw), nil
}

// helpersIsEmptyFilterResult reports whether a gjson.Result represents a
// "the filter found nothing" outcome: the path doesn't exist, or it exists
// but carries no raw JSON representation. Pure function — no I/O — so it's
// unit-testable without a live gjson query.
func helpersIsEmptyFilterResult(result gjson.Result) bool {
	return !result.Exists() || result.Raw == ""
}

// helpersHasContent reports whether raw JSON data is non-empty and not a
// bare "null"/"[]"/"{}" — i.e. there was actually something for a --jq
// filter to plausibly match against, so an empty filter result is more
// likely a syntax mistake than a legitimately empty dataset.
func helpersHasContent(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	switch string(trimmed) {
	case "null", "[]", "{}":
		return false
	}
	return true
}
