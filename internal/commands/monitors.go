package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	monitorsTags   string
	monitorsStatus string
	monitorsQuery  string
	monitorsSort   string
)

func init() {
	rootCmd.AddCommand(monitorsCmd)
	monitorsCmd.AddCommand(monitorsListCmd)
	monitorsCmd.AddCommand(monitorsGetCmd)
	monitorsCmd.AddCommand(monitorsSearchCmd)
	monitorsCmd.AddCommand(monitorsMuteCmd)
	monitorsCmd.AddCommand(monitorsUnmuteCmd)

	monitorsListCmd.Flags().StringVar(&monitorsTags, "tags", "", "Filter by tags (e.g., env:production)")
	monitorsListCmd.Flags().StringVar(&monitorsStatus, "status", "", "Filter by status (alert, warn, ok, no_data)")

	monitorsSearchCmd.Flags().StringVar(&monitorsQuery, "query", "", "Search query (title, status, team, priority, tag, notification)")
	monitorsSearchCmd.Flags().StringVar(&monitorsSort, "sort", "", "Sort by field (title, status, id, type, created)")
}

var monitorsCmd = &cobra.Command{
	Use:   "monitors",
	Short: "Manage Datadog monitors",
}

var monitorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List monitors",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if monitorsTags != "" {
			params.Set("monitor_tags", monitorsTags)
		}
		if limitFlag > 0 {
			params.Set("page_size", strconv.Itoa(limitFlag))
		}

		data, err := c.Get(context.Background(), "api/v1/monitor", params)
		if err != nil {
			return err
		}

		// Filter by status client-side if requested
		if monitorsStatus != "" {
			data = filterByField(data, "overall_state", monitorsStatus)
		}

		return printData("monitors.list", data)
	},
}

var monitorsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a monitor by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/monitor/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var monitorsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search monitors with rich query syntax",
	Long: `Search monitors using Datadog's monitor search API.

Examples:
  ddx monitors search --query "status:alert"
  ddx monitors search --query "notification:slack-logs-team-ops"
  ddx monitors search --query "priority:p2 AND (title:cpu OR title:memory)"
  ddx monitors search --query "team:backend muted:false"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if monitorsQuery != "" {
			params.Set("query", monitorsQuery)
		}
		if monitorsSort != "" {
			params.Set("sort", monitorsSort)
		}
		params.Set("per_page", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v1/monitor/search", params)
		if err != nil {
			return err
		}

		// Extract monitors array from search response
		var resp struct {
			Monitors json.RawMessage `json:"monitors"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.Monitors != nil {
			data = resp.Monitors
		}

		return printData("monitors.search", data)
	},
}

var monitorsMuteCmd = &cobra.Command{
	Use:   "mute <id>",
	Short: "Mute a monitor",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Post(context.Background(), "api/v1/monitor/"+args[0]+"/mute", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var monitorsUnmuteCmd = &cobra.Command{
	Use:   "unmute <id>",
	Short: "Unmute a monitor",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Post(context.Background(), "api/v1/monitor/"+args[0]+"/unmute", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

// filterByField filters a JSON array, keeping only objects where field == value.
func filterByField(data json.RawMessage, field, value string) json.RawMessage {
	var items []map[string]any
	if json.Unmarshal(data, &items) != nil {
		return data
	}
	var filtered []map[string]any
	for _, item := range items {
		if v, ok := item[field]; ok {
			if s, ok := v.(string); ok && s == value {
				filtered = append(filtered, item)
			}
		}
	}
	if filtered == nil {
		filtered = []map[string]any{}
	}
	result, _ := json.Marshal(filtered)
	return result
}
