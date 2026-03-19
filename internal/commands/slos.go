package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(slosCmd)
	slosCmd.AddCommand(slosListCmd)
	slosCmd.AddCommand(slosGetCmd)
	slosCmd.AddCommand(slosHistoryCmd)
}

var slosCmd = &cobra.Command{
	Use:   "slos",
	Short: "Manage Service Level Objectives",
}

var slosListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SLOs",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/slo", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var slosGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get SLO details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/slo/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var slosHistoryCmd = &cobra.Command{
	Use:   "history <id>",
	Short: "Get SLO history",
	Args:  cobra.ExactArgs(1),
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

		params := url.Values{}
		params.Set("from_ts", timeToISO(from))
		params.Set("to_ts", timeToISO(to))

		data, err := c.Get(context.Background(), "api/v1/slo/"+args[0]+"/history", params)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
