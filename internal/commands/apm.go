package commands

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var (
	apmService string
	apmEnv     string
)

func init() {
	rootCmd.AddCommand(apmCmd)
	apmCmd.AddCommand(apmEndpointsCmd)
	apmCmd.AddCommand(apmErrorsCmd)
	apmCmd.AddCommand(apmDependenciesCmd)
	apmCmd.AddCommand(apmStatsCmd)

	apmStatsCmd.Flags().StringVar(&apmService, "service", "", "Service name (required)")
	apmStatsCmd.Flags().StringVar(&apmEnv, "env", "production", "Environment")
	apmStatsCmd.MarkFlagRequired("service")

	apmEndpointsCmd.Flags().StringVar(&apmService, "service", "", "Service name (required)")
	apmEndpointsCmd.Flags().StringVar(&apmEnv, "env", "production", "Environment")
	apmEndpointsCmd.MarkFlagRequired("service")

	apmErrorsCmd.Flags().StringVar(&apmService, "service", "", "Service name (required)")
	apmErrorsCmd.Flags().StringVar(&apmEnv, "env", "production", "Environment")
	apmErrorsCmd.MarkFlagRequired("service")

	apmDependenciesCmd.Flags().StringVar(&apmService, "service", "", "Service name (required)")
	apmDependenciesCmd.Flags().StringVar(&apmEnv, "env", "production", "Environment")
	apmDependenciesCmd.MarkFlagRequired("service")
}

var apmCmd = &cobra.Command{
	Use:   "apm",
	Short: "APM service-level metrics — endpoints, errors, dependencies",
}

var apmEndpointsCmd = &cobra.Command{
	Use:   "endpoints",
	Short: "List top endpoints for a service with stats",
	Long: `List endpoints for a service with request rates, error rates, and latency.

Examples:
  ddx apm endpoints --service web-1000farmacie
  ddx apm endpoints --service web-1000farmacie --env staging`,
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

		// Use spans aggregate to get endpoint stats
		body := spanSearchBody(
			fmt.Sprintf("service:%s env:%s", apmService, apmEnv),
			from, to, limitFlag,
		)

		body = spanAggregateBody(
			fmt.Sprintf("service:%s env:%s", apmService, apmEnv),
			from, to, "resource_name", limitFlag,
		)

		data, err := c.Post(context.Background(), "api/v2/spans/analytics/aggregate", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var apmErrorsCmd = &cobra.Command{
	Use:   "errors",
	Short: "List top errors for a service",
	Long: `List top error types for a service.

Examples:
  ddx apm errors --service web-1000farmacie
  ddx apm errors --service web-1000farmacie --from 24h`,
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

		body := spanAggregateBody(
			fmt.Sprintf("service:%s env:%s status:error", apmService, apmEnv),
			from, to, "resource_name", limitFlag,
		)

		data, err := c.Post(context.Background(), "api/v2/spans/analytics/aggregate", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var apmDependenciesCmd = &cobra.Command{
	Use:   "dependencies",
	Short: "List downstream dependencies for a service",
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
		params.Set("env", apmEnv)
		params.Set("start", fmt.Sprintf("%d", from))
		params.Set("end", fmt.Sprintf("%d", to))

		data, err := c.Get(context.Background(), "api/v1/service_dependencies", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var apmStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Service stats with latency percentiles (p50/p75/p99)",
	Long: `Get service-level stats including request count, error count, and latency percentiles.

Examples:
  ddx apm stats --service web-1000farmacie
  ddx apm stats --service web-1000farmacie --from 4h`,
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
			"data": map[string]any{
				"type": "aggregate_request",
				"attributes": map[string]any{
					"filter": map[string]any{
						"query": fmt.Sprintf("service:%s env:%s", apmService, apmEnv),
						"from":  timeToISO(from),
						"to":    timeToISO(to),
					},
					"compute": []map[string]any{
						{"type": "total", "aggregation": "count"},
						{"type": "total", "aggregation": "avg", "metric": "@duration"},
						{"type": "total", "aggregation": "pc50", "metric": "@duration"},
						{"type": "total", "aggregation": "pc75", "metric": "@duration"},
						{"type": "total", "aggregation": "pc99", "metric": "@duration"},
					},
					"group_by": []map[string]any{
						{
							"facet": "resource_name",
							"limit": limitFlag,
							"sort":  map[string]any{"order": "desc"},
						},
					},
				},
			},
		}

		data, err := c.Post(context.Background(), "api/v2/spans/analytics/aggregate", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

func spanAggregateBody(query string, from, to int64, groupByFacet string, limit int) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type": "aggregate_request",
			"attributes": map[string]any{
				"filter": map[string]any{
					"query": query,
					"from":  timeToISO(from),
					"to":    timeToISO(to),
				},
				"compute": []map[string]any{
					{"type": "total", "aggregation": "count"},
				},
				"group_by": []map[string]any{
					{
						"facet": groupByFacet,
						"limit": limit,
						"sort":  map[string]any{"order": "desc"},
					},
				},
			},
		},
	}
}
