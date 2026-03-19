package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(usageCmd)
	usageCmd.AddCommand(usageSummaryCmd)
}

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Datadog usage metrics",
}

var usageSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Get usage summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("start_month", timeToISO(from))

		data, err := c.Get(context.Background(), "api/v1/usage/summary", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}
