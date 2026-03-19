package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	incidentsQuery       string
	incidentsSort        string
	incidentTimeline     bool
	incidentTimelineFrom string
	incidentTimelineTo   string
	incidentsFacets      bool
)

func init() {
	rootCmd.AddCommand(incidentsCmd)
	incidentsCmd.AddCommand(incidentsListCmd)
	incidentsCmd.AddCommand(incidentsGetCmd)
	incidentsCmd.AddCommand(incidentsFacetsCmd)

	incidentsListCmd.Flags().StringVar(&incidentsQuery, "query", "state:active", "Search query (state, severity, team, commander, etc.)")
	incidentsListCmd.Flags().StringVar(&incidentsSort, "sort", "-created", "Sort field (created, -created, resolved, -severity, etc.)")

	incidentsGetCmd.Flags().BoolVar(&incidentTimeline, "timeline", false, "Include timeline with comments and status changes")
	incidentsGetCmd.Flags().StringVar(&incidentTimelineFrom, "timeline-from", "", "Filter timeline entries after this time")
	incidentsGetCmd.Flags().StringVar(&incidentTimelineTo, "timeline-to", "", "Filter timeline entries before this time")

	incidentsFacetsCmd.Flags().StringVar(&incidentsQuery, "query", "state:active", "Search query for facet aggregation")
}

var incidentsCmd = &cobra.Command{
	Use:   "incidents",
	Short: "Search and inspect incidents",
}

var incidentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List incidents with rich query syntax",
	Long: `List incidents with faceted search.

Examples:
  ddx incidents list
  ddx incidents list --query "state:active severity:SEV-1"
  ddx incidents list --query "(state:active OR state:stable) AND team:backend"
  ddx incidents list --query "customer_impacted:true"
  ddx incidents list --query "commander.handle:user@example.com"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("query", incidentsQuery)
		params.Set("sort", incidentsSort)
		params.Set("page[size]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
		if err != nil {
			return err
		}

		incidents := extractIncidents(data)
		return printData("incidents.list", incidents)
	},
}

var incidentsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get incident details with optional timeline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if incidentTimeline {
			params.Set("include", "timeline")
		}

		data, err := c.Get(context.Background(), "api/v2/incidents/"+args[0], params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var incidentsFacetsCmd = &cobra.Command{
	Use:   "facets",
	Short: "Get faceted breakdown of incidents",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("query", incidentsQuery)
		params.Set("facets", "true")

		data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

func extractIncidents(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Data != nil {
		// Flatten incidents from data[].attributes
		var items []json.RawMessage
		if json.Unmarshal(wrapper.Data, &items) == nil {
			var result []map[string]any
			for _, item := range items {
				var obj struct {
					ID         string          `json:"id"`
					Attributes json.RawMessage `json:"attributes"`
				}
				if json.Unmarshal(item, &obj) == nil {
					var attrs map[string]any
					if json.Unmarshal(obj.Attributes, &attrs) == nil {
						attrs["id"] = obj.ID
						result = append(result, attrs)
					}
				}
			}
			if result != nil {
				out, _ := json.Marshal(result)
				return out
			}
		}
		return wrapper.Data
	}
	return raw
}
