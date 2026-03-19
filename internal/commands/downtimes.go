package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(downtimesCmd)
	downtimesCmd.AddCommand(downtimesListCmd)
	downtimesCmd.AddCommand(downtimesGetCmd)
	downtimesCmd.AddCommand(downtimesCancelCmd)
}

var downtimesCmd = &cobra.Command{
	Use:   "downtimes",
	Short: "Manage maintenance downtimes",
}

var downtimesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List downtimes",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/downtime", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var downtimesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get downtime details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/downtime/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var downtimesCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a downtime",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/downtime/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Downtime %s cancelled\n", args[0])
		}
		return nil
	},
}
