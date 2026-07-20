package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/nicolasacchi/ddx/internal/timeparse"
	"github.com/spf13/cobra"
)

var (
	tracesQuery       string
	tracesCustomAttr  string
	tracesService     string
	traceServiceEntry bool
	traceIncludePath  string
)

func init() {
	rootCmd.AddCommand(tracesCmd)
	tracesCmd.AddCommand(tracesSearchCmd)
	tracesCmd.AddCommand(tracesListCmd)
	tracesCmd.AddCommand(tracesscalarWaterfallCmd)

	tracesSearchCmd.Flags().StringVar(&tracesQuery, "query", "", "Span search query (e.g., service:web status:error)")
	tracesSearchCmd.Flags().StringVar(&tracesCustomAttr, "custom-attrs", "", "Comma-separated wildcard patterns for custom attributes")
	tracesSearchCmd.MarkFlagRequired("query")

	tracesListCmd.Flags().StringVar(&tracesService, "service", "", "Filter by service name")
}

var tracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "Search and inspect APM traces and spans",
}

var tracesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search spans across traces",
	Long: `Search spans using Datadog query syntax.

Examples:
  ddx traces search --query "service:web-1000farmacie status:error" --from 1h
  ddx traces search --query "@duration:>5000000" --from 4h
  ddx traces search --query "service:(web OR api) status:error" --from 24h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}
		to, err := parseTo()
		if err != nil {
			return err
		}

		body := spanSearchBody(tracesQuery, from, to, limitFlag)
		data, err := c.Post(context.Background(), "api/v2/spans/events/search", body)
		if err != nil {
			return err
		}

		if verboseFlag {
			explorerURL := buildExplorerURL("traces", tracesQuery, from, to)
			fmt.Fprintln(cmd.ErrOrStderr(), "Explorer:", explorerURL)
		}

		return printData("", extractWithMeta(data, "spans"))
	},
}

func spanSearchBody(query string, from, to int64, limit int) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type": "search_request",
			"attributes": map[string]any{
				"filter": map[string]any{
					"query": query,
					"from":  timeToISO(from),
					"to":    timeToISO(to),
				},
				"sort": "-timestamp",
				"page": map[string]any{
					"limit": limit,
				},
			},
		},
	}
}

var tracesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent traces",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}
		to, err := parseTo()
		if err != nil {
			return err
		}

		q := "*"
		if tracesService != "" {
			q = "service:" + tracesService
		}

		body := spanSearchBody(q, from, to, limitFlag)
		data, err := c.Post(context.Background(), "api/v2/spans/events/search", body)
		if err != nil {
			return err
		}

		return printData("", extractWithMeta(data, "spans"))
	},
}

var tracesGetCmd = &cobra.Command{
	Use:   "get <trace-id>",
	Short: "Get a trace by ID with optional hierarchy view",
	Long: `Fetch a complete trace by its ID.

Use --service-entry-only to collapse internal spans to service boundaries.
Use --include-path to filter spans matching a specific service.

Examples:
  ddx traces get 0123456789abcdef0123456789abcdef
  ddx traces get 0123456789abcdef --service-entry-only
  ddx traces get 0123456789abcdef --include-path "service:web-1000farmacie"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, _ := parseTimeValue("48h")
		to, _ := parseTimeValue("now")
		body := spanSearchBody(fmt.Sprintf("trace_id:%s", args[0]), from, to, 200)
		data, err := c.Post(context.Background(), "api/v2/spans/events/search", body)
		if err != nil {
			return err
		}

		spans := extractData(data)

		// Service-entry-only: group spans by service, show only service boundaries
		if traceServiceEntry {
			spans = collapseToServiceEntry(spans)
		}

		// Include-path: filter spans matching a specific query
		if traceIncludePath != "" {
			spans = filterSpansByPath(spans, traceIncludePath)
		}

		// Show explorer URL
		if verboseFlag {
			explorerURL := buildExplorerURL("traces", "trace_id:"+args[0], from, to)
			fmt.Fprintln(cmd.ErrOrStderr(), "Explorer:", explorerURL)
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "tip: for a hierarchical span tree, use `ddx traces waterfall %s`\n", args[0])

		return printData("", spans)
	},
}

func init() {
	tracesCmd.AddCommand(tracesGetCmd)
	tracesGetCmd.Flags().BoolVar(&traceServiceEntry, "service-entry-only", false, "Collapse spans to service boundaries")
	tracesGetCmd.Flags().StringVar(&traceIncludePath, "include-path", "", "Filter spans (e.g., service:web-1000farmacie)")
}

// collapseToServiceEntry groups spans by service, keeping only unique services with counts.
func collapseToServiceEntry(data json.RawMessage) json.RawMessage {
	var items []map[string]any
	if json.Unmarshal(data, &items) != nil {
		return data
	}

	serviceMap := map[string]map[string]any{}
	for _, item := range items {
		svc := "unknown"
		if attrs, ok := item["attributes"].(map[string]any); ok {
			if s, ok := attrs["service"].(string); ok {
				svc = s
			}
		}
		if _, exists := serviceMap[svc]; !exists {
			serviceMap[svc] = map[string]any{
				"service":    svc,
				"span_count": 0,
			}
		}
		serviceMap[svc]["span_count"] = serviceMap[svc]["span_count"].(int) + 1
	}

	var result []map[string]any
	for _, v := range serviceMap {
		result = append(result, v)
	}

	out, _ := json.Marshal(result)
	return out
}

// filterSpansByPath keeps only spans where attributes match a key:value pattern.
func filterSpansByPath(data json.RawMessage, path string) json.RawMessage {
	parts := splitColon(path)
	if len(parts) != 2 {
		return data
	}
	key, value := parts[0], parts[1]

	var items []map[string]any
	if json.Unmarshal(data, &items) != nil {
		return data
	}

	var filtered []map[string]any
	for _, item := range items {
		if attrs, ok := item["attributes"].(map[string]any); ok {
			if v, ok := attrs[key].(string); ok && v == value {
				filtered = append(filtered, item)
			}
		}
	}
	if filtered == nil {
		filtered = []map[string]any{}
	}

	out, _ := json.Marshal(filtered)
	return out
}

func splitColon(s string) []string {
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}

func parseTimeValue(s string) (int64, error) {
	return timeparse.Parse(s)
}

// tracesscalarWaterfallCmd — GET api/v2/trace/{trace_id} (operationId
// GetTraceByID, verified against openapi-v2.yaml: 60 req/min, x-unstable,
// no pagination — `is_truncated` signals server-side payload overflow
// instead). Reconstructs the span tree client-side and renders it as a
// waterfall — the last high-value MCP fallback for trace inspection.
var tracesscalarWaterfallCmd = &cobra.Command{
	Use:   "waterfall <trace-id>",
	Short: "Render a full trace as an indented span-tree waterfall",
	Long: `Fetch the complete trace (every span, via GetTraceByID) and reconstruct the
parent/child span tree client-side, rendered as a waterfall.

Default output (TTY/human) is indented text: one line per span with
service, resource (operation name), and duration in ms — marked [ERROR]
when the span carries an error flag. Pass --json (or pipe the output) for
a nested JSON tree instead: {"span": {...}, "children": [...]}.

--limit caps the number of spans rendered (pre-order across the whole
tree, root's children first); truncation is always noted on stderr, never
silently dropped. Spans whose parent isn't in the trace (orphans) and true
trace-roots both attach under a synthetic root, so nothing is dropped from
the render even when Datadog returns a partial/re-parented set.

Examples:
  ddx traces waterfall 0000000000000000abc1230000000000
  ddx traces waterfall 0000000000000000abc1230000000000 --json
  ddx traces waterfall 0000000000000000abc1230000000000 --limit 20`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/trace/"+args[0], nil)
		if err != nil {
			return err
		}

		var resp tracesscalarTraceResponseRaw
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("parse trace response: %w", err)
		}

		if resp.Data.Attributes.IsTruncated {
			fmt.Fprintln(cmd.ErrOrStderr(), "traces waterfall: response truncated by Datadog (payload exceeded max size) — some spans may be missing")
		}

		totalSpans := len(resp.Data.Attributes.Spans)
		root := tracesscalarBuildSpanTree(resp.Data.Attributes.Spans)

		if isJSONMode() {
			count := 0
			tree := tracesscalarNodeToJSON(root, limitFlag, &count)
			if limitFlag > 0 && count < totalSpans {
				fmt.Fprintf(cmd.ErrOrStderr(), "traces waterfall: rendered %d of %d spans (--limit %d)\n", count, totalSpans, limitFlag)
			}
			out, err := json.Marshal(tree)
			if err != nil {
				return err
			}
			return printData("", out)
		}

		text, rendered, truncated := tracesscalarRenderWaterfallText(root, limitFlag)
		if truncated {
			fmt.Fprintf(cmd.ErrOrStderr(), "traces waterfall: rendered %d of %d spans (--limit %d)\n", rendered, totalSpans, limitFlag)
		}
		fmt.Fprint(cmd.OutOrStdout(), text)
		return nil
	},
}

// tracesscalarSpan is one entry of TraceDataAttributes.spans
// (components.schemas.APMTraceSpan in openapi-v2.yaml). Required fields
// per the spec: service, name, resource, traceID, spanID, parentID.
type tracesscalarSpan struct {
	SpanID      int64              `json:"spanID"`
	ParentID    int64              `json:"parentID"`
	TraceID     int64              `json:"traceID"`
	TraceIDFull string             `json:"traceIDFull"`
	Name        string             `json:"name"`
	Resource    string             `json:"resource"`
	Service     string             `json:"service"`
	Type        string             `json:"type"`
	StartTime   int64              `json:"startTime"`
	EndTime     int64              `json:"endTime"`
	Duration    int64              `json:"duration"`
	Error       int                `json:"error"`
	Meta        map[string]string  `json:"meta,omitempty"`
	Metrics     map[string]float64 `json:"metrics,omitempty"`
}

// tracesscalarTraceResponseRaw models the GetTraceByID response
// (components.schemas.TraceResponse / TraceData / TraceDataAttributes).
type tracesscalarTraceResponseRaw struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			IsTruncated bool               `json:"is_truncated"`
			Spans       []tracesscalarSpan `json:"spans"`
		} `json:"attributes"`
	} `json:"data"`
}

// tracesscalarSpanNode is one node of the client-side reconstructed span
// tree. IsRoot marks the synthetic root that trace-roots (parentID == 0)
// and orphans (parentID pointing at a span not present in the payload)
// both attach under — it carries no real span data of its own.
type tracesscalarSpanNode struct {
	Span     tracesscalarSpan
	Children []*tracesscalarSpanNode
	IsRoot   bool
}

// tracesscalarBuildSpanTree indexes spans by span_id and attaches children
// via parent_id; spans whose parent_id is 0, self-referential, or does not
// resolve to another span in the set (orphans) attach under a synthetic
// root instead of being dropped. Children of every node are sorted by
// start time. Pure — no I/O — unit-tested against a fabricated span set
// (root, nested children, orphan).
func tracesscalarBuildSpanTree(spans []tracesscalarSpan) *tracesscalarSpanNode {
	byID := make(map[int64]*tracesscalarSpanNode, len(spans))
	for i := range spans {
		byID[spans[i].SpanID] = &tracesscalarSpanNode{Span: spans[i]}
	}

	root := &tracesscalarSpanNode{IsRoot: true}
	for _, sp := range spans {
		node := byID[sp.SpanID]
		parent, ok := byID[sp.ParentID]
		if sp.ParentID == 0 || !ok || sp.ParentID == sp.SpanID {
			root.Children = append(root.Children, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}

	tracesscalarSortSpanChildren(root)
	return root
}

// tracesscalarSortSpanChildren recursively sorts every node's children by
// start time, stably (so same-timestamp spans keep their original order).
func tracesscalarSortSpanChildren(n *tracesscalarSpanNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		return n.Children[i].Span.StartTime < n.Children[j].Span.StartTime
	})
	for _, c := range n.Children {
		tracesscalarSortSpanChildren(c)
	}
}

// tracesscalarRenderWaterfallText renders the tree as indented text: one
// line per span, "<indent>service resource (name) duration_ms[ERROR]".
// limit <= 0 means unlimited; otherwise rendering stops in pre-order once
// "limit" spans have been printed. Returns the rendered text, how many
// spans were actually rendered, and whether the render was cut short.
func tracesscalarRenderWaterfallText(root *tracesscalarSpanNode, limit int) (text string, rendered int, truncated bool) {
	var b strings.Builder
	count := 0
	cut := false

	var walk func(n *tracesscalarSpanNode, depth int)
	walk = func(n *tracesscalarSpanNode, depth int) {
		for _, c := range n.Children {
			if cut {
				return
			}
			if limit > 0 && count >= limit {
				cut = true
				return
			}
			count++
			errMark := ""
			if c.Span.Error != 0 {
				errMark = " [ERROR]"
			}
			durMs := float64(c.Span.Duration) / 1e6
			fmt.Fprintf(&b, "%s%s %s (%s) %.2fms%s\n", strings.Repeat("  ", depth), c.Span.Service, c.Span.Resource, c.Span.Name, durMs, errMark)
			walk(c, depth+1)
		}
	}
	walk(root, 0)

	return b.String(), count, cut
}

// tracesscalarNodeToJSON renders the tree as nested JSON:
// {"span": {...} | null, "children": [...]}. The synthetic root's "span" is
// JSON null. limit <= 0 means unlimited; count is threaded through the
// recursion (pre-order, root's children first) so the cap applies across
// the whole tree rather than per-branch.
func tracesscalarNodeToJSON(n *tracesscalarSpanNode, limit int, count *int) map[string]any {
	out := map[string]any{}
	if n.IsRoot {
		out["span"] = nil
	} else {
		out["span"] = tracesscalarSpanToMap(n.Span)
	}

	children := []map[string]any{}
	for _, c := range n.Children {
		if limit > 0 && *count >= limit {
			break
		}
		*count++
		children = append(children, tracesscalarNodeToJSON(c, limit, count))
	}
	out["children"] = children
	return out
}

// tracesscalarSpanToMap converts a span to a plain map for JSON output,
// including a convenience duration_ms field alongside the raw nanosecond
// duration.
func tracesscalarSpanToMap(sp tracesscalarSpan) map[string]any {
	return map[string]any{
		"spanID":      sp.SpanID,
		"parentID":    sp.ParentID,
		"traceID":     sp.TraceID,
		"traceIDFull": sp.TraceIDFull,
		"service":     sp.Service,
		"name":        sp.Name,
		"resource":    sp.Resource,
		"type":        sp.Type,
		"startTime":   sp.StartTime,
		"endTime":     sp.EndTime,
		"duration":    sp.Duration,
		"duration_ms": float64(sp.Duration) / 1e6,
		"error":       sp.Error,
		"meta":        sp.Meta,
		"metrics":     sp.Metrics,
	}
}
