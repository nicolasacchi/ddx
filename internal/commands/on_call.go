package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var onCallTeam string

func init() {
	rootCmd.AddCommand(onCallCmd)
	onCallCmd.AddCommand(onCallTeamsCmd)
	onCallCmd.AddCommand(onCallSchedulesCmd)

	onCallSchedulesCmd.Flags().StringVar(&onCallTeam, "team", "", "Filter by team")
}

var onCallCmd = &cobra.Command{
	Use:   "on-call",
	Short: "On-call teams and schedules",
}

var onCallTeamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "List on-call teams",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/on-call/teams", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var onCallSchedulesCmd = &cobra.Command{
	Use:   "schedules",
	Short: "List on-call schedules",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		if onCallTeam != "" {
			params.Set("filter[team]", onCallTeam)
		}
		data, err := c.Get(context.Background(), "api/v2/on-call/schedules", params)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}
