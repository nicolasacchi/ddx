package commands

import (
	"context"
	"fmt"

	"github.com/nicolasacchi/ddx/internal/timeparse"
	"github.com/spf13/cobra"
)

var (
	tracesQuery      string
	tracesCustomAttr string
	tracesService    string
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

		return printData("", extractData(data))
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
	Short: "Get a trace by ID with full span hierarchy",
	Long: `Fetch a complete trace by its ID.

The trace ID should be 32 hex characters or 1-39 decimal digits.

Examples:
  ddx traces get 0123456789abcdef0123456789abcdef
  ddx traces get 1234567890`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, _ := parseTimeValue("48h")
		to, _ := parseTimeValue("now")
		body := spanSearchBody(fmt.Sprintf("trace_id:%s", args[0]), from, to, 100)
		data, err := c.Post(context.Background(), "api/v2/spans/events/search", body)
		if err != nil {
			return err
		}

		return printData("", extractData(data))
	},
}

func init() {
	tracesCmd.AddCommand(tracesGetCmd)
}

func parseTimeValue(s string) (int64, error) {
	return timeparse.Parse(s)
}
