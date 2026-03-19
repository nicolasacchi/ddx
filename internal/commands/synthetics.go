package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(syntheticsCmd)
	syntheticsCmd.AddCommand(syntheticsListCmd)
	syntheticsCmd.AddCommand(syntheticsGetCmd)
}

var syntheticsCmd = &cobra.Command{
	Use:   "synthetics",
	Short: "Manage synthetic tests",
}

var syntheticsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List synthetic tests",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/synthetics/tests", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var syntheticsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get synthetic test details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/synthetics/tests/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
