package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(serviceCatalogCmd)
	serviceCatalogCmd.AddCommand(scListCmd)
	serviceCatalogCmd.AddCommand(scGetCmd)
}

var serviceCatalogCmd = &cobra.Command{
	Use:   "service-catalog",
	Short: "Service catalog registry",
}

var scListCmd = &cobra.Command{
	Use:   "list",
	Short: "List services in catalog",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		params.Set("page[size]", "50")
		data, err := c.Get(context.Background(), "api/v2/services/definitions", params)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var scGetCmd = &cobra.Command{
	Use:   "get <service-name>",
	Short: "Get service catalog entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/services/definitions/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
