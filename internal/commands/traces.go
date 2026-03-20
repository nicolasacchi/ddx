package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nicolasacchi/ddx/internal/timeparse"
	"github.com/spf13/cobra"
)

var (
	tracesQuery        string
	tracesCustomAttr   string
	tracesService      string
	traceServiceEntry  bool
	traceIncludePath   string
)

func init() {
	rootCmd.AddCommand(tracesCmd)
	tracesCmd.AddCommand(tracesSearchCmd)
	tracesCmd.AddCommand(tracesListCmd)

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

		return printData("", extractWithMeta(data, "traces"))
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

		return printData("", extractData(data))
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
