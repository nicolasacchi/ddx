package commands

import (
	"context"

	"github.com/spf13/cobra"
)

var securityQuery string

func init() {
	rootCmd.AddCommand(securityCmd)
	securityCmd.AddCommand(securityRulesCmd)
	securityCmd.AddCommand(securitySignalsCmd)

	securitySignalsCmd.Flags().StringVar(&securityQuery, "query", "*", "Signal search query")
}

var securityCmd = &cobra.Command{
	Use:   "security",
	Short: "Security monitoring rules and signals",
}

var securityRulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "List security rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/security_monitoring/rules", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var securitySignalsCmd = &cobra.Command{
	Use:   "signals",
	Short: "Search security signals",
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
				"query": securityQuery,
				"from":  timeToISO(from),
				"to":    timeToISO(to),
			},
			"sort": "-timestamp",
			"page": map[string]any{
				"limit": limitFlag,
			},
		}

		data, err := c.Post(context.Background(), "api/v2/security_monitoring/signals/search", body)
		if err != nil {
			return err
		}

		return printData("", extractData(data))
	},
}
