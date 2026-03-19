package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(cloudCmd)
	cloudCmd.AddCommand(cloudAWSCmd)
	cloudCmd.AddCommand(cloudGCPCmd)
	cloudCmd.AddCommand(cloudAzureCmd)
}

var cloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Cloud integrations (AWS, GCP, Azure)",
}

var cloudAWSCmd = &cobra.Command{
	Use:   "aws",
	Short: "List AWS integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/integration/aws", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var cloudGCPCmd = &cobra.Command{
	Use:   "gcp",
	Short: "List GCP integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/integration/gcp", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var cloudAzureCmd = &cobra.Command{
	Use:   "azure",
	Short: "List Azure integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/integration/azure/host_filters", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
