package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	hostsFilter string
)

func init() {
	rootCmd.AddCommand(hostsCmd)
	hostsCmd.AddCommand(hostsListCmd)

	hostsListCmd.Flags().StringVar(&hostsFilter, "filter", "", "Filter hosts by name (substring match)")
}

var hostsCmd = &cobra.Command{
	Use:   "hosts",
	Short: "List and query infrastructure hosts",
}

var hostsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List monitored hosts",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if hostsFilter != "" {
			params.Set("filter", hostsFilter)
		}
		params.Set("count", strconv.Itoa(limitFlag))
		params.Set("include_muted_hosts_data", "true")

		data, err := c.Get(context.Background(), "api/v1/hosts", params)
		if err != nil {
			return err
		}

		// Extract host_list from response
		var resp struct {
			HostList json.RawMessage `json:"host_list"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.HostList != nil {
			data = resp.HostList
		}

		return printData("", data)
	},
}
