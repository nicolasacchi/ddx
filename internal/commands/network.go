package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(networkCmd)
	networkCmd.AddCommand(networkDevicesCmd)
	networkCmd.AddCommand(networkFlowsCmd)
}

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Network devices and flows",
}

var networkDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List network devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/ndm/devices", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var networkFlowsCmd = &cobra.Command{
	Use:   "flows",
	Short: "List network flows",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/network/flows", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}
