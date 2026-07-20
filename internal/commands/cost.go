package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	costQueries  []string
	costFormulas []string
	costTags     string

	usagecostAttrMonth         string
	usagecostAttrEndMonth      string
	usagecostAttrFields        string
	usagecostAttrSortDirection string
	usagecostAttrSortName      string
)

func init() {
	rootCmd.AddCommand(costCmd)
	costCmd.AddCommand(costQueryCmd)
	costCmd.AddCommand(costAttributionCmd)
	costCmd.AddCommand(usagecostDimensionsCmd)

	// StringArrayVar (not StringSliceVar) + splitQueriesTopLevel: a query/
	// formula's own tag filter ("avg:a{x:1,y:2}") contains commas that must
	// NOT be treated as flag-value separators. StringSliceVar would split on
	// every comma; StringArrayVar takes each --queries occurrence verbatim,
	// and splitQueriesTopLevel then splits only the top-level commas a
	// caller may have comma-joined into a single occurrence.
	costQueryCmd.Flags().StringArrayVar(&costQueries, "queries", nil, "Cloud cost metric queries (e.g. \"sum:aws.cost.amortized{service:ec2}\")")
	costQueryCmd.Flags().StringArrayVar(&costFormulas, "formulas", nil, "Formula expressions (e.g. \"top(query0, 5, 'mean', 'desc')\")")
	costQueryCmd.MarkFlagRequired("queries")

	costAttributionCmd.Flags().StringVar(&usagecostAttrMonth, "month", "", "ISO month YYYY-MM (or RFC3339): start_month, and end_month unless --end-month overrides it (required)")
	costAttributionCmd.MarkFlagRequired("month")
	costAttributionCmd.Flags().StringVar(&usagecostAttrEndMonth, "end-month", "", "ISO month YYYY-MM (or RFC3339) to end the range at; defaults to --month (single-month query)")
	costAttributionCmd.Flags().StringVar(&usagecostAttrFields, "fields", "*", `Comma-separated cost fields, e.g. "infra_host_on_demand_cost,infra_host_percentage_in_account"; "*" retrieves all (default)`)
	costAttributionCmd.Flags().StringVar(&costTags, "tags", "", "Comma-separated tag keys to break cost down by (tag_breakdown_keys)")
	costAttributionCmd.Flags().StringVar(&usagecostAttrSortDirection, "sort-direction", "", "asc or desc (always sorted by total cost)")
	costAttributionCmd.Flags().StringVar(&usagecostAttrSortName, "sort-name", "", "Billing dimension to sort by, e.g. infra_host")
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

		queries := splitQueriesTopLevel(costQueries)
		formulas := splitQueriesTopLevel(costFormulas)

		queryList := make([]map[string]any, len(queries))
		for i, q := range queries {
			queryList[i] = map[string]any{
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
					"queries": queryList,
				},
			},
		}

		if len(formulas) > 0 {
			formulaList := make([]map[string]any, len(formulas))
			for i, f := range formulas {
				formulaList[i] = map[string]any{"formula": f}
			}
			body["data"].(map[string]any)["attributes"].(map[string]any)["formulas"] = formulaList
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
	Short: "Get monthly cost attribution by tag",
	Long: `Get monthly cost attribution by tag across your account
(api/v2/cost_by_tag/monthly_cost_attribution).

Cost attribution data for a given month becomes available no later than the
19th of the following month. Parent-level organizations only; not available
on the Government (US1-FED) site.

Auto-paginates via meta.pagination.next_record_id, capped at 10 pages, with a
5-second sleep between page requests per Datadog's stated rate-limit
guidance for this endpoint's pagination.

Examples:
  ddx cost attribution --month 2026-06
  ddx cost attribution --month 2026-01 --end-month 2026-06 --fields "infra_host_on_demand_cost,infra_host_percentage_in_account"
  ddx cost attribution --month 2026-06 --tags team,env --sort-name infra_host --sort-direction desc`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		startMonth, err := ucParseMonth(usagecostAttrMonth)
		if err != nil {
			return err
		}
		if startMonth == "" {
			return fmt.Errorf("--month is required")
		}
		endMonth := startMonth
		if usagecostAttrEndMonth != "" {
			endMonth, err = ucParseMonth(usagecostAttrEndMonth)
			if err != nil {
				return err
			}
		}

		items, err := usagecostPaginateCursor(func(cursor string) ([]json.RawMessage, string, error) {
			params := usagecostBuildAttributionParams(startMonth, endMonth, usagecostAttrFields, costTags, usagecostAttrSortDirection, usagecostAttrSortName, cursor)
			data, err := c.Get(context.Background(), "api/v2/cost_by_tag/monthly_cost_attribution", params)
			if err != nil {
				return nil, "", err
			}
			return usagecostExtractCursorPage(data)
		}, 10, time.Sleep)
		if err != nil {
			return err
		}

		out, err := json.Marshal(items)
		if err != nil {
			return err
		}
		usagecostReportCount("cost attribution", len(items))
		return printData("cost.attribution", out)
	},
}

var usagecostDimensionsCmd = &cobra.Command{
	Use:   "dimensions",
	Short: "Get active billing dimensions for cost attribution",
	Long: `Get active billing dimensions for cost attribution
(api/v2/cost_by_tag/active_billing_dimensions).

Use the returned dimension names to build --fields for "ddx cost attribution"
(e.g. "<dimension>_on_demand_cost,<dimension>_percentage_in_account"). Cost
data for a given month becomes available no later than the 19th of the
following month.

Examples:
  ddx cost dimensions`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/cost_by_tag/active_billing_dimensions", nil)
		if err != nil {
			return err
		}

		item := usagecostFlattenV2Single(extractData(data))
		return printData("cost.dimensions", item)
	},
}

// usagecostBuildAttributionParams constructs the query params for
// api/v2/cost_by_tag/monthly_cost_attribution. fields defaults to "*" (all
// fields) when empty. tagsCSV is a comma-separated list of tag keys, trimmed
// and re-joined into tag_breakdown_keys; empty entries are dropped. cursor
// is the next_record_id to resume from ("" for the first page).
func usagecostBuildAttributionParams(startMonth, endMonth, fields, tagsCSV, sortDirection, sortName, cursor string) url.Values {
	params := url.Values{}
	params.Set("start_month", startMonth)
	params.Set("end_month", endMonth)

	if fields == "" {
		fields = "*"
	}
	params.Set("fields", fields)

	if tagsCSV != "" {
		var keys []string
		for _, tag := range strings.Split(tagsCSV, ",") {
			if t := strings.TrimSpace(tag); t != "" {
				keys = append(keys, t)
			}
		}
		if len(keys) > 0 {
			params.Set("tag_breakdown_keys", strings.Join(keys, ","))
		}
	}

	if sortDirection != "" {
		params.Set("sort_direction", sortDirection)
	}
	if sortName != "" {
		params.Set("sort_name", sortName)
	}
	if cursor != "" {
		params.Set("next_record_id", cursor)
	}

	return params
}
