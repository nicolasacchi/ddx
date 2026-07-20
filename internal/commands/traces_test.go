package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

// tracesscalarFabricatedSpans builds a small, deterministic span set for
// tree-building/rendering tests:
//
//	root (synthetic)
//	├─ A (spanID 1, parentID 0 — true trace root)
//	│  ├─ C (spanID 3, parentID 1, startTime 1050 — starts before B)
//	│  └─ B (spanID 2, parentID 1, startTime 1100, error flag set)
//	│     └─ D (spanID 4, parentID 2 — nested grandchild)
//	└─ E (spanID 5, parentID 999 — orphan: 999 does not exist in the set)
func tracesscalarFabricatedSpans() []tracesscalarSpan {
	return []tracesscalarSpan{
		{
			SpanID: 1, ParentID: 0, TraceID: 42,
			Service: "web", Resource: "GET /products", Name: "web.request",
			StartTime: 1000, Duration: 500_000_000, Error: 0,
		},
		{
			SpanID: 2, ParentID: 1, TraceID: 42,
			Service: "web-store", Resource: "SELECT", Name: "db.query",
			StartTime: 1100, Duration: 120_000_000, Error: 1,
		},
		{
			SpanID: 3, ParentID: 1, TraceID: 42,
			Service: "cache", Resource: "GET", Name: "redis.command",
			StartTime: 1050, Duration: 5_000_000, Error: 0,
		},
		{
			SpanID: 4, ParentID: 2, TraceID: 42,
			Service: "mysql", Resource: "connect", Name: "db.connect",
			StartTime: 1120, Duration: 10_000_000, Error: 0,
		},
		{
			SpanID: 5, ParentID: 999, TraceID: 42,
			Service: "orphan-svc", Resource: "ORPHAN", Name: "orphan.op",
			StartTime: 2000, Duration: 1_000_000, Error: 0,
		},
	}
}

func TestTracesscalarBuildSpanTree(t *testing.T) {
	root := tracesscalarBuildSpanTree(tracesscalarFabricatedSpans())

	if !root.IsRoot {
		t.Fatalf("root.IsRoot = false, want true")
	}
	if len(root.Children) != 2 {
		t.Fatalf("root has %d children, want 2 (true root A + orphan E)", len(root.Children))
	}

	a := root.Children[0]
	e := root.Children[1]
	if a.Span.SpanID != 1 {
		t.Fatalf("root.Children[0].SpanID = %d, want 1 (A)", a.Span.SpanID)
	}
	if e.Span.SpanID != 5 {
		t.Fatalf("root.Children[1].SpanID = %d, want 5 (orphan E)", e.Span.SpanID)
	}

	if len(a.Children) != 2 {
		t.Fatalf("A has %d children, want 2 (C, B)", len(a.Children))
	}
	// C (startTime 1050) must sort before B (startTime 1100).
	if a.Children[0].Span.SpanID != 3 {
		t.Fatalf("A.Children[0].SpanID = %d, want 3 (C, earlier startTime)", a.Children[0].Span.SpanID)
	}
	if a.Children[1].Span.SpanID != 2 {
		t.Fatalf("A.Children[1].SpanID = %d, want 2 (B, later startTime)", a.Children[1].Span.SpanID)
	}

	b := a.Children[1]
	if len(b.Children) != 1 || b.Children[0].Span.SpanID != 4 {
		t.Fatalf("B.Children = %#v, want single child D (spanID 4)", b.Children)
	}

	c := a.Children[0]
	if len(c.Children) != 0 {
		t.Fatalf("C has %d children, want 0 (leaf)", len(c.Children))
	}
	if len(e.Children) != 0 {
		t.Fatalf("orphan E has %d children, want 0 (leaf)", len(e.Children))
	}
}

func TestTracesscalarBuildSpanTree_emptyInput(t *testing.T) {
	root := tracesscalarBuildSpanTree(nil)
	if !root.IsRoot || len(root.Children) != 0 {
		t.Fatalf("empty span set should build a childless synthetic root, got %#v", root)
	}
}

func TestTracesscalarRenderWaterfallText(t *testing.T) {
	root := tracesscalarBuildSpanTree(tracesscalarFabricatedSpans())

	t.Run("unlimited renders pre-order: A, C, B, D, E", func(t *testing.T) {
		text, rendered, truncated := tracesscalarRenderWaterfallText(root, 0)
		if truncated {
			t.Fatalf("truncated = true, want false for unlimited render")
		}
		if rendered != 5 {
			t.Fatalf("rendered = %d, want 5", rendered)
		}
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("got %d lines, want 5:\n%s", len(lines), text)
		}

		// Depth 0: A
		if !strings.HasPrefix(lines[0], "web GET /products (web.request) 500.00ms") {
			t.Fatalf("line 0 = %q, want A's line at depth 0", lines[0])
		}
		// Depth 1: C (indented once, no error marker)
		if !strings.HasPrefix(lines[1], "  cache GET (redis.command) 5.00ms") || strings.Contains(lines[1], "[ERROR]") {
			t.Fatalf("line 1 = %q, want C's line at depth 1, no error marker", lines[1])
		}
		// Depth 1: B, with error marker
		if !strings.HasPrefix(lines[2], "  web-store SELECT (db.query) 120.00ms [ERROR]") {
			t.Fatalf("line 2 = %q, want B's line at depth 1 with [ERROR]", lines[2])
		}
		// Depth 2: D, nested under B
		if !strings.HasPrefix(lines[3], "    mysql connect (db.connect) 10.00ms") {
			t.Fatalf("line 3 = %q, want D's line at depth 2", lines[3])
		}
		// Depth 0: orphan E, back at root level
		if !strings.HasPrefix(lines[4], "orphan-svc ORPHAN (orphan.op) 1.00ms") {
			t.Fatalf("line 4 = %q, want orphan E's line at depth 0", lines[4])
		}
	})

	t.Run("limit truncates pre-order traversal and reports it", func(t *testing.T) {
		text, rendered, truncated := tracesscalarRenderWaterfallText(root, 3)
		if !truncated {
			t.Fatalf("truncated = false, want true when limit < total spans")
		}
		if rendered != 3 {
			t.Fatalf("rendered = %d, want 3", rendered)
		}
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("got %d lines, want 3:\n%s", len(lines), text)
		}
		if strings.Contains(text, "db.connect") || strings.Contains(text, "orphan.op") {
			t.Fatalf("truncated render leaked spans past the limit:\n%s", text)
		}
	})

	t.Run("limit at or above total is not truncated", func(t *testing.T) {
		_, rendered, truncated := tracesscalarRenderWaterfallText(root, 5)
		if truncated {
			t.Fatalf("truncated = true, want false when limit == total spans")
		}
		if rendered != 5 {
			t.Fatalf("rendered = %d, want 5", rendered)
		}
	})
}

func TestTracesscalarNodeToJSON(t *testing.T) {
	root := tracesscalarBuildSpanTree(tracesscalarFabricatedSpans())

	t.Run("unlimited tree shape", func(t *testing.T) {
		count := 0
		tree := tracesscalarNodeToJSON(root, 0, &count)

		if tree["span"] != nil {
			t.Fatalf("root span = %#v, want nil (synthetic root)", tree["span"])
		}
		children, ok := tree["children"].([]map[string]any)
		if !ok || len(children) != 2 {
			t.Fatalf("root children = %#v, want 2 entries (A, orphan E)", tree["children"])
		}

		aNode := children[0]
		aSpan, ok := aNode["span"].(map[string]any)
		if !ok {
			t.Fatalf("A node span not a map: %#v", aNode["span"])
		}
		if aSpan["service"] != "web" || aSpan["resource"] != "GET /products" {
			t.Fatalf("A span = %#v, want service=web resource=GET /products", aSpan)
		}
		if aSpan["duration_ms"] != 500.0 {
			t.Fatalf("A duration_ms = %#v, want 500", aSpan["duration_ms"])
		}

		aChildren := aNode["children"].([]map[string]any)
		if len(aChildren) != 2 {
			t.Fatalf("A has %d JSON children, want 2", len(aChildren))
		}
		// C before B (sorted by start time).
		cSpan := aChildren[0]["span"].(map[string]any)
		if cSpan["service"] != "cache" {
			t.Fatalf("A.Children[0] service = %v, want cache (C)", cSpan["service"])
		}
		bSpan := aChildren[1]["span"].(map[string]any)
		if bSpan["service"] != "web-store" || bSpan["error"] != 1 {
			t.Fatalf("A.Children[1] = %#v, want web-store with error=1 (B)", bSpan)
		}

		bChildren := aChildren[1]["children"].([]map[string]any)
		if len(bChildren) != 1 {
			t.Fatalf("B has %d JSON children, want 1 (D)", len(bChildren))
		}

		eNode := children[1]
		eSpan := eNode["span"].(map[string]any)
		if eSpan["service"] != "orphan-svc" {
			t.Fatalf("orphan E span = %#v, want service=orphan-svc", eSpan)
		}

		if count != 5 {
			t.Fatalf("count = %d, want 5 spans total", count)
		}
	})

	t.Run("limit caps the total node count across the tree", func(t *testing.T) {
		count := 0
		tree := tracesscalarNodeToJSON(root, 2, &count)
		if count != 2 {
			t.Fatalf("count = %d, want 2 (capped)", count)
		}
		children := tree["children"].([]map[string]any)
		if len(children) != 1 {
			t.Fatalf("root children = %d, want 1 when capped at 2 (A only, orphan E excluded)", len(children))
		}
		aChildren := children[0]["children"].([]map[string]any)
		if len(aChildren) != 1 {
			t.Fatalf("A children = %d, want 1 when capped at 2 (only C)", len(aChildren))
		}
	})

	t.Run("childless node renders an empty (non-nil) children array", func(t *testing.T) {
		count := 0
		leaf := &tracesscalarSpanNode{Span: tracesscalarSpan{SpanID: 9, Service: "svc"}}
		node := tracesscalarNodeToJSON(leaf, 0, &count)
		children, ok := node["children"].([]map[string]any)
		if !ok || children == nil || len(children) != 0 {
			t.Fatalf("leaf children = %#v, want empty non-nil slice", node["children"])
		}
	})
}

func TestTracesscalarSpanToMap(t *testing.T) {
	sp := tracesscalarSpan{
		SpanID: 7, ParentID: 1, TraceID: 42, TraceIDFull: "abc123",
		Service: "web", Name: "web.request", Resource: "GET /x", Type: "web",
		StartTime: 100, EndTime: 200, Duration: 100_000_000, Error: 1,
		Meta:    map[string]string{"env": "production"},
		Metrics: map[string]float64{"http.status_code": 200},
	}
	m := tracesscalarSpanToMap(sp)

	if m["spanID"] != uint64(7) || m["service"] != "web" || m["error"] != 1 {
		t.Fatalf("tracesscalarSpanToMap() = %#v, missing/wrong core fields", m)
	}
	if m["duration_ms"] != 100.0 {
		t.Fatalf("duration_ms = %#v, want 100", m["duration_ms"])
	}
	meta, ok := m["meta"].(map[string]string)
	if !ok || meta["env"] != "production" {
		t.Fatalf("meta = %#v, want {env: production}", m["meta"])
	}
}

func TestTracesscalarSpanUnmarshalsOversizedUint64SpanID(t *testing.T) {
	// Regression: 9876543210987654321 exceeds math.MaxInt64
	// (9223372036854775807, the spec's own example value) — a real span with
	// an ID that large used to hard-fail json.Unmarshal when the field was
	// declared int64 instead of uint64.
	raw := []byte(`{"spanID": 9876543210987654321, "parentID": 1, "traceID": 42, "service": "web"}`)
	var sp tracesscalarSpan
	if err := json.Unmarshal(raw, &sp); err != nil {
		t.Fatalf("unmarshal oversized spanID: %v", err)
	}
	if sp.SpanID != 9876543210987654321 {
		t.Fatalf("SpanID = %d, want 9876543210987654321", sp.SpanID)
	}
}

func TestTracesscalarBuildSpanTreeWithOversizedSpanID(t *testing.T) {
	spans := []tracesscalarSpan{
		{SpanID: 1, ParentID: 0, TraceID: 42, Service: "web"},
		{SpanID: 9876543210987654321, ParentID: 1, TraceID: 42, Service: "downstream"},
	}
	root := tracesscalarBuildSpanTree(spans)
	if len(root.Children) != 1 {
		t.Fatalf("root has %d children, want 1", len(root.Children))
	}
	a := root.Children[0]
	if a.Span.SpanID != 1 {
		t.Fatalf("root child SpanID = %d, want 1", a.Span.SpanID)
	}
	if len(a.Children) != 1 || a.Children[0].Span.SpanID != 9876543210987654321 {
		t.Fatalf("A.Children = %#v, want single child with oversized spanID", a.Children)
	}
}
