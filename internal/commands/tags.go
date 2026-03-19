package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var tagsSource string

func init() {
	rootCmd.AddCommand(tagsCmd)
	tagsCmd.AddCommand(tagsListCmd)
	tagsCmd.AddCommand(tagsHostsCmd)

	tagsListCmd.Flags().StringVar(&tagsSource, "source", "", "Filter by tag source")
}

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Manage host tags",
}

var tagsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		if tagsSource != "" {
			params.Set("source", tagsSource)
		}
		data, err := c.Get(context.Background(), "api/v1/tags/hosts", params)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var tagsHostsCmd = &cobra.Command{
	Use:   "hosts <hostname>",
	Short: "Get tags for a host",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/tags/hosts/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
