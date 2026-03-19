package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(casesCmd)
	casesCmd.AddCommand(casesListCmd)
	casesCmd.AddCommand(casesGetCmd)
}

var casesCmd = &cobra.Command{
	Use:   "cases",
	Short: "Case management",
}

var casesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cases",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/cases", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var casesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get case details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/cases/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
