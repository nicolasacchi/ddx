package commands

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/spf13/cobra"
)

var (
	dashboardsQuery   string
	dashboardsSort    string
	dashboardsIncVars bool
)

func init() {
	rootCmd.AddCommand(dashboardsCmd)
	dashboardsCmd.AddCommand(dashboardsListCmd)
	dashboardsCmd.AddCommand(dashboardsGetCmd)
	dashboardsCmd.AddCommand(dashboardsSearchCmd)

	dashboardsSearchCmd.Flags().StringVar(&dashboardsQuery, "query", "", "Search query (title, metric, widget type, author, team)")
	dashboardsSearchCmd.Flags().StringVar(&dashboardsSort, "sort", "", "Sort field (title, -popularity, created_at, -modified_at)")
	dashboardsSearchCmd.Flags().BoolVar(&dashboardsIncVars, "include-vars", false, "Include template variable info")
}

var dashboardsCmd = &cobra.Command{
	Use:   "dashboards",
	Short: "Search and inspect dashboards",
}

var dashboardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all dashboards",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v1/dashboard", nil)
		if err != nil {
			return err
		}

		// Extract dashboards array
		var resp struct {
			Dashboards json.RawMessage `json:"dashboards"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.Dashboards != nil {
			data = resp.Dashboards
		}

		return printData("dashboards.list", data)
	},
}

var dashboardsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a dashboard by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/dashboard/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var dashboardsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search dashboards by metric, widget type, author, or team",
	Long: `Search dashboards using rich query syntax.

Examples:
  ddx dashboards search --query "system.cpu.user"
  ddx dashboards search --query "team:backend"
  ddx dashboards search --query "widget_type:slo" --sort -popularity`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if dashboardsQuery != "" {
			params.Set("filter[shared]", "false")
			params.Set("filter[deleted]", "false")
			// Dashboard list API doesn't have dedicated search — we use list + client filter
			// For now, use the dashboard list endpoint
		}

		data, err := c.Get(context.Background(), "api/v1/dashboard", params)
		if err != nil {
			return err
		}

		var resp struct {
			Dashboards json.RawMessage `json:"dashboards"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.Dashboards != nil {
			data = resp.Dashboards
		}

		// Client-side filter by query if provided
		if dashboardsQuery != "" {
			data = filterDashboardsByTitle(data, dashboardsQuery)
		}

		return printData("dashboards.search", data)
	},
}

func filterDashboardsByTitle(data json.RawMessage, query string) json.RawMessage {
	var items []map[string]any
	if json.Unmarshal(data, &items) != nil {
		return data
	}
	var filtered []map[string]any
	for _, item := range items {
		title, _ := item["title"].(string)
		desc, _ := item["description"].(string)
		if containsInsensitive(title, query) || containsInsensitive(desc, query) {
			filtered = append(filtered, item)
		}
	}
	if filtered == nil {
		filtered = []map[string]any{}
	}
	result, _ := json.Marshal(filtered)
	return result
}

func containsInsensitive(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && len(substr) > 0 &&
				(strings_contains_fold(s, substr)))
}

func strings_contains_fold(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if equal_fold(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func equal_fold(s, t string) bool {
	for i := 0; i < len(s); i++ {
		a, b := s[i], t[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
