package commands

import (
	"context"

	"github.com/spf13/cobra"
)

var (
	eventsQuery string
	eventsSort  string
)

func init() {
	rootCmd.AddCommand(eventsCmd)
	eventsCmd.AddCommand(eventsListCmd)
	eventsCmd.AddCommand(eventsSearchCmd)

	eventsListCmd.Flags().StringVar(&eventsQuery, "query", "*", "Event query (source, tags, message)")
	eventsListCmd.Flags().StringVar(&eventsSort, "sort", "-timestamp", "Sort: timestamp or -timestamp")

	eventsSearchCmd.Flags().StringVar(&eventsQuery, "query", "", "Event search query (required)")
	eventsSearchCmd.Flags().StringVar(&eventsSort, "sort", "-timestamp", "Sort order")
	eventsSearchCmd.MarkFlagRequired("query")
}

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Search events (alerts, deployments, custom)",
}

var eventsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent events",
	RunE:  runEventsSearch,
}

var eventsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search events with query",
	Long: `Search events using Datadog query syntax.

Examples:
  ddx events search --query "source:nagios" --from 24h
  ddx events search --query "env:prod" --from 7d`,
	RunE: runEventsSearch,
}

func runEventsSearch(cmd *cobra.Command, args []string) error {
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
			"query": eventsQuery,
			"from":  timeToISO(from),
			"to":    timeToISO(to),
		},
		"sort": eventsSort,
		"page": map[string]any{
			"limit": limitFlag,
		},
	}

	data, err := c.Post(context.Background(), "api/v2/events/search", body)
	if err != nil {
		return err
	}

	return printData("", extractData(data))
}
