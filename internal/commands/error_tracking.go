package commands

import (
	"context"

	"github.com/spf13/cobra"
)

var (
	etQuery string
	etTrack string
)

func init() {
	rootCmd.AddCommand(errorTrackingCmd)
	errorTrackingCmd.AddCommand(etIssuesCmd)
	etIssuesCmd.AddCommand(etIssuesSearchCmd)
	etIssuesCmd.AddCommand(etIssuesGetCmd)

	etIssuesSearchCmd.Flags().StringVar(&etQuery, "query", "", "Filter query (e.g., service:1000farmacie)")
	etIssuesSearchCmd.Flags().StringVar(&etTrack, "persona", "backend", "Error source: backend, frontend, mobile")
}

var errorTrackingCmd = &cobra.Command{
	Use:   "error-tracking",
	Short: "Search and inspect error tracking issues",
}

var etIssuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "Manage error tracking issues",
}

var etIssuesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search error tracking issues",
	Long: `Search error tracking issues.

Examples:
  ddx error-tracking issues search --from 1d
  ddx error-tracking issues search --query "service:1000farmacie" --from 7d
  ddx error-tracking issues search --from 1d --limit 10`,
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

		q := etQuery
		if q == "" {
			q = "*"
		}

		body := map[string]any{
			"data": map[string]any{
				"type": "search_request",
				"attributes": map[string]any{
					"query":   q,
					"from":    from * 1000,
					"to":      to * 1000,
					"sort":    "-error_count",
					"persona": etTrack,
					"page": map[string]any{
						"limit": limitFlag,
					},
				},
			},
		}

		data, err := c.Post(context.Background(), "api/v2/error-tracking/issues/search", body)
		if err != nil {
			return err
		}

		issues := extractWithMeta(data, "error-tracking")
		flattened := flattenV2Items(issues)

		// Client-side limit enforcement (API may ignore limit)
		if limitFlag > 0 {
			flattened = truncateArray(flattened, limitFlag)
		}

		return printData("error-tracking.search", flattened)
	},
}

var etIssuesGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Get error tracking issue details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/error-tracking/issues/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
