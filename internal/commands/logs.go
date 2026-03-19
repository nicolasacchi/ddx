package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	logsQuery      string
	logsSort       string
	logsStorage    string
	logsCompute    string
	logsGroupBy    string
	logsExtraField string
)

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.AddCommand(logsSearchCmd)
	logsCmd.AddCommand(logsAggregateCmd)

	logsSearchCmd.Flags().StringVar(&logsQuery, "query", "*", "Datadog search query")
	logsSearchCmd.Flags().StringVar(&logsSort, "sort", "-timestamp", "Sort order: timestamp or -timestamp")
	logsSearchCmd.Flags().StringVar(&logsStorage, "storage", "", "Storage tier: indexes, flex, online-archives")

	logsAggregateCmd.Flags().StringVar(&logsQuery, "query", "*", "Datadog search query")
	logsAggregateCmd.Flags().StringVar(&logsCompute, "compute", "count", "Aggregation: count, avg(@field), sum(@field), min(@field), max(@field)")
	logsAggregateCmd.Flags().StringVar(&logsGroupBy, "group-by", "", "Field to group by (e.g., service, @http.status_code)")
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Search and analyze logs",
}

var logsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search logs with Datadog query syntax",
	Long: `Search logs using Datadog query syntax.

Examples:
  ddx logs search --query "status:error" --from 1h
  ddx logs search --query "service:web-1000farmacie kube_namespace:backend-prod" --from 4h
  ddx logs search --query "@http.status_code:500" --from 24h --limit 20`,
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

		body := map[string]any{
			"filter": map[string]any{
				"query": logsQuery,
				"from":  fmt.Sprintf("%d000", from), // milliseconds
				"to":    fmt.Sprintf("%d000", to),
			},
			"sort": logsSort,
			"page": map[string]any{
				"limit": limitFlag,
			},
		}

		if logsStorage != "" {
			body["filter"].(map[string]any)["storage_tier"] = logsStorage
		}

		data, err := c.Post(context.Background(), "api/v2/logs/events/search", body)
		if err != nil {
			return err
		}

		// Extract the logs array from response
		logs := extractData(data)
		return printData("logs.search", logs)
	},
}

var logsAggregateCmd = &cobra.Command{
	Use:   "aggregate",
	Short: "Aggregate logs with server-side grouping",
	Long: `Aggregate logs with server-side compute and grouping.

Examples:
  ddx logs aggregate --query "status:error" --compute "count" --group-by "service" --from 1h
  ddx logs aggregate --query "service:web" --compute "avg(@duration)" --group-by "@http.status_code" --from 4h`,
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

		// Parse compute spec: "count", "avg(@duration)", "sum(@field)"
		computeObj := map[string]any{
			"aggregation": logsCompute,
		}
		// Handle "avg(@field)" style — extract aggregation + metric
		if idx := indexOf(logsCompute, '('); idx > 0 && logsCompute[len(logsCompute)-1] == ')' {
			computeObj["aggregation"] = logsCompute[:idx]
			computeObj["metric"] = logsCompute[idx+1 : len(logsCompute)-1]
		}

		fromISO := timeToISO(from)
		toISO := timeToISO(to)

		body := map[string]any{
			"filter": map[string]any{
				"query": logsQuery,
				"from":  fromISO,
				"to":    toISO,
			},
			"compute": []map[string]any{computeObj},
		}

		if logsGroupBy != "" {
			body["group_by"] = []map[string]any{
				{
					"facet": logsGroupBy,
					"limit": limitFlag,
					"sort":  map[string]any{"order": "desc"},
				},
			}
		}

		data, err := c.Post(context.Background(), "api/v2/logs/analytics/aggregate", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

func timeToISO(unix int64) string {
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// extractData unwraps {"data": [...]} from Datadog v2 API responses.
func extractData(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Data != nil {
		return wrapper.Data
	}
	return raw
}
