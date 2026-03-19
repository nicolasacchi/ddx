package commands

import (
	"context"

	"github.com/spf13/cobra"
)

var auditQuery string

func init() {
	rootCmd.AddCommand(auditLogsCmd)
	auditLogsCmd.AddCommand(auditLogsSearchCmd)

	auditLogsSearchCmd.Flags().StringVar(&auditQuery, "query", "*", "Audit log query")
}

var auditLogsCmd = &cobra.Command{
	Use:   "audit-logs",
	Short: "Search audit logs",
}

var auditLogsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search audit events",
	Long: `Search audit events for security and compliance.

Examples:
  ddx audit-logs search --from 24h
  ddx audit-logs search --query "@action:modified" --from 7d`,
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
				"query": auditQuery,
				"from":  timeToISO(from),
				"to":    timeToISO(to),
			},
			"sort": "-timestamp",
			"page": map[string]any{
				"limit": limitFlag,
			},
		}

		data, err := c.Post(context.Background(), "api/v2/audit/events/search", body)
		if err != nil {
			return err
		}

		return printData("", extractData(data))
	},
}
