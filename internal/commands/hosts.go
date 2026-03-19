package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	hostsFilter  string
	hostsTag     string
	hostsGroupBy string
	hostsSortBy  string
)

func init() {
	rootCmd.AddCommand(hostsCmd)
	hostsCmd.AddCommand(hostsListCmd)
	hostsCmd.AddCommand(hostsQueryCmd)

	hostsListCmd.Flags().StringVar(&hostsFilter, "filter", "", "Filter hosts by name (substring match)")

	hostsQueryCmd.Flags().StringVar(&hostsTag, "tag", "", "Filter by tag (e.g., env:production, cluster_name:prod-apps-green)")
	hostsQueryCmd.Flags().StringVar(&hostsGroupBy, "group-by", "", "Group results by field (e.g., instance_type, os)")
	hostsQueryCmd.Flags().StringVar(&hostsSortBy, "sort", "", "Sort by field (e.g., name, apps)")
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

		var resp struct {
			HostList json.RawMessage `json:"host_list"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.HostList != nil {
			data = resp.HostList
		}

		return printData("", data)
	},
}

var hostsQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query hosts with tag filtering and grouping",
	Long: `Query hosts with tag-based filtering and optional grouping.

Examples:
  ddx hosts query --tag "env:production"
  ddx hosts query --tag "cluster_name:prod-apps-green" --group-by instance_type
  ddx hosts query --tag "env:production" --group-by os`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if hostsTag != "" {
			params.Set("filter", hostsTag)
		}
		params.Set("count", "1000") // fetch all for grouping
		params.Set("include_muted_hosts_data", "true")

		data, err := c.Get(context.Background(), "api/v1/hosts", params)
		if err != nil {
			return err
		}

		var resp struct {
			HostList []map[string]any `json:"host_list"`
			Total    int              `json:"total_matching"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return printData("", data)
		}

		// Filter by tag if the API filter wasn't precise enough
		if hostsTag != "" && strings.Contains(hostsTag, ":") {
			filtered := filterHostsByTag(resp.HostList, hostsTag)
			resp.HostList = filtered
		}

		// Group by field if requested
		if hostsGroupBy != "" {
			groups := groupHostsBy(resp.HostList, hostsGroupBy)
			out, _ := json.Marshal(groups)
			return printData("", out)
		}

		out, _ := json.Marshal(resp.HostList)
		return printData("", out)
	},
}

func filterHostsByTag(hosts []map[string]any, tag string) []map[string]any {
	var result []map[string]any
	for _, h := range hosts {
		if hostHasTag(h, tag) {
			result = append(result, h)
		}
	}
	return result
}

func hostHasTag(host map[string]any, tag string) bool {
	// Check tags_by_source — each source has an array of tag strings
	if tbs, ok := host["tags_by_source"].(map[string]any); ok {
		for _, tags := range tbs {
			if tagArr, ok := tags.([]any); ok {
				for _, t := range tagArr {
					if s, ok := t.(string); ok && s == tag {
						return true
					}
				}
			}
		}
	}
	return false
}

func groupHostsBy(hosts []map[string]any, field string) map[string]int {
	groups := map[string]int{}
	for _, h := range hosts {
		key := extractHostField(h, field)
		groups[key]++
	}
	return groups
}

func extractHostField(host map[string]any, field string) string {
	// Direct field lookup
	if v, ok := host[field]; ok {
		return fmt.Sprintf("%v", v)
	}
	// Check meta
	if meta, ok := host["meta"].(map[string]any); ok {
		if v, ok := meta[field]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	// Check tags for key:value
	if tbs, ok := host["tags_by_source"].(map[string]any); ok {
		for _, tags := range tbs {
			if tagArr, ok := tags.([]any); ok {
				for _, t := range tagArr {
					if s, ok := t.(string); ok && strings.HasPrefix(s, field+":") {
						return s[len(field)+1:]
					}
				}
			}
		}
	}
	return "unknown"
}
