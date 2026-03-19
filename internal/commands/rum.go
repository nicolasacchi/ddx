package commands

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	rumQuery    string
	rumDetailed bool
)

func init() {
	rootCmd.AddCommand(rumCmd)
	rumCmd.AddCommand(rumAppsCmd)
	rumCmd.AddCommand(rumEventsCmd)
	rumCmd.AddCommand(rumSessionsCmd)

	rumEventsCmd.Flags().StringVar(&rumQuery, "query", "@type:error", "RUM event query")
	rumEventsCmd.Flags().BoolVar(&rumDetailed, "detailed", false, "Return full event data")

	rumSessionsCmd.Flags().StringVar(&rumQuery, "query", "@type:session", "RUM session query")
}

var rumCmd = &cobra.Command{
	Use:   "rum",
	Short: "Real User Monitoring — apps, events, sessions",
}

var rumAppsCmd = &cobra.Command{
	Use:   "apps",
	Short: "List RUM applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/rum/applications", nil)
		if err != nil {
			return err
		}

		// Extract data array and flatten
		items := extractData(data)
		return printData("rum.apps", flattenV2Items(items))
	},
}

var rumEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Search RUM events (all types: view, action, error, resource, etc.)",
	Long: `Search RUM events across all event types.

Event types: session, view, action, error, resource, long_task, vital

Examples:
  ddx rum events --query "@type:error" --from 1h
  ddx rum events --query "@type:view @view.loading_time:>5000" --from 24h
  ddx rum events --query "@application.name:\"1000Farmacie\" @type:error" --from 4h
  ddx rum events --query "@user.id:123" --from 7d --detailed
  ddx rum events --query "@type:resource @view.url:*/checkout/*" --from 1h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}
		to, err := parseTo()
		if err != nil {
			return err
		}

		body := map[string]any{
			"filter": map[string]any{
				"query": rumQuery,
				"from":  fmt.Sprintf("%d000", from),
				"to":    fmt.Sprintf("%d000", to),
			},
			"sort": "-timestamp",
			"page": map[string]any{
				"limit": limitFlag,
			},
		}

		data, err := c.Post(context.Background(), "api/v2/rum/events/search", body)
		if err != nil {
			return err
		}

		result := extractData(data)

		// Add explorer URL
		if verboseFlag || rumDetailed {
			explorerURL := buildExplorerURL("rum", rumQuery, from, to)
			fmt.Fprintln(cmd.ErrOrStderr(), "Explorer:", explorerURL)
		}

		return printData("", result)
	},
}

var rumSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List RUM sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}
		to, err := parseTo()
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("filter[query]", rumQuery)
		params.Set("filter[from]", strconv.FormatInt(from, 10)+"000")
		params.Set("filter[to]", strconv.FormatInt(to, 10)+"000")
		params.Set("page[limit]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/rum/events", params)
		if err != nil {
			return err
		}

		return printData("", extractData(data))
	},
}
