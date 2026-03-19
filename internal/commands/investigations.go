package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(investigationsCmd)
	investigationsCmd.AddCommand(investigationsListCmd)
	investigationsCmd.AddCommand(investigationsGetCmd)
}

var investigationsCmd = &cobra.Command{
	Use:   "investigations",
	Short: "Security investigations",
}

var investigationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List investigations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/security_monitoring/investigations", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var investigationsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get investigation details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/security_monitoring/investigations/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
