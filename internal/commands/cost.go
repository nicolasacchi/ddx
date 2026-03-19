package commands

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	costQueries  []string
	costFormulas []string
	costTags     string
)

func init() {
	rootCmd.AddCommand(costCmd)
	costCmd.AddCommand(costQueryCmd)
	costCmd.AddCommand(costAttributionCmd)

	costQueryCmd.Flags().StringSliceVar(&costQueries, "queries", nil, "Cloud cost metric queries")
	costQueryCmd.Flags().StringSliceVar(&costFormulas, "formulas", nil, "Formula expressions")
	costQueryCmd.MarkFlagRequired("queries")

	costAttributionCmd.Flags().StringVar(&costTags, "tags", "", "Comma-separated tag keys for attribution")
}

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Cloud cost management",
}

var costQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query cloud cost metrics",
	Long: `Query cloud cost metrics using the timeseries API.

Examples:
  ddx cost query --queries "sum:all.cost{*}.rollup(sum, daily)" --from 7d
  ddx cost query --queries "sum:aws.cost.amortized{service:ec2}" --from 30d`,
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

		queries := make([]map[string]any, len(costQueries))
		for i, q := range costQueries {
			queries[i] = map[string]any{
				"name":        "query" + strconv.Itoa(i),
				"data_source": "cloud_cost",
				"query":       q,
			}
		}

		body := map[string]any{
			"data": map[string]any{
				"type": "timeseries_request",
				"attributes": map[string]any{
					"from":    from * 1000,
					"to":      to * 1000,
					"queries": queries,
				},
			},
		}

		if len(costFormulas) > 0 {
			formulas := make([]map[string]any, len(costFormulas))
			for i, f := range costFormulas {
				formulas[i] = map[string]any{"formula": f}
			}
			body["data"].(map[string]any)["attributes"].(map[string]any)["formulas"] = formulas
		}

		data, err := c.Post(context.Background(), "api/v2/query/timeseries", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var costAttributionCmd = &cobra.Command{
	Use:   "attribution",
	Short: "Get cost attribution by tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		from, err := parseFrom()
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("start_month", timeToISO(from))
		if costTags != "" {
			for _, tag := range strings.Split(costTags, ",") {
				params.Add("tag_breakdown_keys", strings.TrimSpace(tag))
			}
		}

		data, err := c.Get(context.Background(), "api/v1/usage/cost_by_org", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}
