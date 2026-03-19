package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(scorecardsCmd)
	scorecardsCmd.AddCommand(scorecardsListCmd)
	scorecardsCmd.AddCommand(scorecardsRulesCmd)
}

var scorecardsCmd = &cobra.Command{
	Use:   "scorecards",
	Short: "Service scorecards and rules",
}

var scorecardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scorecards",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/scorecards", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var scorecardsRulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "List scorecard rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/scorecards/rules", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}
