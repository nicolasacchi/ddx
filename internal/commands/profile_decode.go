package commands

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ============================================================================
// Flame graph decoder for `ddx profile aggregate --by function`
//
// The /profiling/api/v1/aggregate response packs a single flame-graph tree
// using:
//
//   nodeSchema  = ["frame", "value", "gini", "children"]
//   frameSchema = ["schema", "kind", "library", "function", "file", "line"]
//
// Each node is a JSON array [frame_idx, value, gini, children]. Each frame is
// an array of integers indexed into the strings[] table — except the `line`
// field which is a literal integer (not an index).
//
// We walk the tree, sum each leaf node's value by frame index, resolve the
// frame back to function/file/line via the strings table, and emit top-N.
// ============================================================================

// flameNode is one decoded node in the flame graph tree.
type flameNode struct {
	FrameIdx int
	Value    float64
	Gini     float64
	Children []flameNode
}

// parseFlameNode decodes a packed [frame, value, gini, children] array.
// Children are parsed recursively. A node with no children is treated as a leaf.
func parseFlameNode(raw json.RawMessage) (flameNode, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return flameNode{}, fmt.Errorf("node not an array: %w", err)
	}
	if len(arr) < 4 {
		return flameNode{}, fmt.Errorf("expected node array of 4 elements, got %d", len(arr))
	}
	var n flameNode
	if err := json.Unmarshal(arr[0], &n.FrameIdx); err != nil {
		return n, fmt.Errorf("frame index: %w", err)
	}
	if err := json.Unmarshal(arr[1], &n.Value); err != nil {
		return n, fmt.Errorf("value: %w", err)
	}
	if err := json.Unmarshal(arr[2], &n.Gini); err != nil {
		return n, fmt.Errorf("gini: %w", err)
	}
	var children []json.RawMessage
	if err := json.Unmarshal(arr[3], &children); err != nil {
		return n, fmt.Errorf("children: %w", err)
	}
	n.Children = make([]flameNode, 0, len(children))
	for i, c := range children {
		child, err := parseFlameNode(c)
		if err != nil {
			return n, fmt.Errorf("child[%d]: %w", i, err)
		}
		n.Children = append(n.Children, child)
	}
	return n, nil
}

// frameInfo holds the resolved fields of one frame.
type frameInfo struct {
	Function string
	File     string
	Line     int
	Library  string
	Kind     string
}

// resolveFrames builds frameInfo per frame index from the packed frames array
// and the interned strings table, using frameSchema to map field name → position.
func resolveFrames(frames [][]int, schema []string, strings []string) []frameInfo {
	pos := func(name string) int {
		for i, s := range schema {
			if s == name {
				return i
			}
		}
		return -1
	}
	getStr := func(i int) string {
		if i >= 0 && i < len(strings) {
			return strings[i]
		}
		return ""
	}

	functionPos := pos("function")
	filePos := pos("file")
	linePos := pos("line")
	libPos := pos("library")
	kindPos := pos("kind")

	out := make([]frameInfo, len(frames))
	for i, f := range frames {
		var info frameInfo
		if functionPos >= 0 && functionPos < len(f) {
			info.Function = getStr(f[functionPos])
		}
		if filePos >= 0 && filePos < len(f) {
			info.File = getStr(f[filePos])
		}
		if linePos >= 0 && linePos < len(f) {
			// `line` is a LITERAL int, not a string index.
			info.Line = f[linePos]
		}
		if libPos >= 0 && libPos < len(f) {
			info.Library = getStr(f[libPos])
		}
		if kindPos >= 0 && kindPos < len(f) {
			info.Kind = getStr(f[kindPos])
		}
		out[i] = info
	}
	return out
}

// collectLeaves walks the tree and aggregates leaf values by frame index.
// A leaf is any node with zero children.
func collectLeaves(n flameNode, agg map[int]float64) {
	if len(n.Children) == 0 {
		agg[n.FrameIdx] += n.Value
		return
	}
	for _, c := range n.Children {
		collectLeaves(c, agg)
	}
}

// leafEntry is one row in the function-view output.
type leafEntry struct {
	Function string  `json:"function"`
	File     string  `json:"file,omitempty"`
	Line     int     `json:"line,omitempty"`
	Library  string  `json:"library,omitempty"`
	Kind     string  `json:"kind,omitempty"`
	Value    float64 `json:"value"`
	Percent  float64 `json:"percent_of_total"`
}

// printProfileFunctionView decodes the packed flame graph from a raw aggregate
// response and emits the top-N hot leaves (frames with no callees) sorted by
// value descending. This is the CLI equivalent of the UI's flame graph leaves.
func printProfileFunctionView(raw json.RawMessage, profType string, topN int) error {
	var resp struct {
		FlameGraph         json.RawMessage    `json:"flameGraph"`
		Frames             [][]int            `json:"frames"`
		Strings            []string           `json:"strings"`
		FrameSchema        []string           `json:"frameSchema"`
		SummaryValues      map[string]float64 `json:"summaryValues"`
		NumberOfProfiles   int                `json:"numberOfProfiles"`
		TotalProfilesCount int                `json:"totalProfilesCount"`
		Metadata           json.RawMessage    `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse aggregate response: %w", err)
	}
	if len(resp.FlameGraph) == 0 {
		return fmt.Errorf("response has no flameGraph; check --service / --query / --from / --type values")
	}

	root, err := parseFlameNode(resp.FlameGraph)
	if err != nil {
		return fmt.Errorf("decode flame graph: %w", err)
	}

	leafByFrame := make(map[int]float64)
	collectLeaves(root, leafByFrame)

	frameInfos := resolveFrames(resp.Frames, resp.FrameSchema, resp.Strings)

	total := resp.SummaryValues[profType]
	if total == 0 {
		// Fall back to summing the leaf values themselves.
		for _, v := range leafByFrame {
			total += v
		}
	}

	leaves := make([]leafEntry, 0, len(leafByFrame))
	for fIdx, value := range leafByFrame {
		var info frameInfo
		if fIdx >= 0 && fIdx < len(frameInfos) {
			info = frameInfos[fIdx]
		}
		entry := leafEntry{
			Function: info.Function,
			File:     info.File,
			Line:     info.Line,
			Library:  info.Library,
			Kind:     info.Kind,
			Value:    value,
		}
		if total > 0 {
			entry.Percent = value / total * 100.0
		}
		leaves = append(leaves, entry)
	}
	sort.SliceStable(leaves, func(i, j int) bool {
		return leaves[i].Value > leaves[j].Value
	})
	if topN > 0 && len(leaves) > topN {
		leaves = leaves[:topN]
	}

	out := map[string]any{
		"profile_type":        profType,
		"profiles_aggregated": resp.NumberOfProfiles,
		"profiles_in_window":  resp.TotalProfilesCount,
		"unique_leaf_frames":  len(leafByFrame),
		"total":               total,
		"top":                 leaves,
		"metadata":            resp.Metadata,
	}
	jsonBytes, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return printData("", jsonBytes)
}
