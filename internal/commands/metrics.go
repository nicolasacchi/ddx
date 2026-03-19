package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	metricsQueries   []string
	metricsFormulas  []string
	metricsInterval  int
	metricsRaw       bool
	metricsCloudCost bool
	metricsNameFilter string
	metricsTagFilter  string
	metricsSubmitName string
	metricsSubmitVal  float64
	metricsSubmitTags string
	metricsCtxTags    bool
	metricsCtxAssets  bool
	metricsCtxScope   string
)

func init() {
	rootCmd.AddCommand(metricsCmd)
	metricsCmd.AddCommand(metricsQueryCmd)
	metricsCmd.AddCommand(metricsListCmd)
	metricsCmd.AddCommand(metricsMetadataCmd)
	metricsCmd.AddCommand(metricsContextCmd)
	metricsCmd.AddCommand(metricsSubmitCmd)

	metricsQueryCmd.Flags().StringSliceVar(&metricsQueries, "queries", nil, "Metric queries (e.g., \"avg:system.cpu.user{*}\")")
	metricsQueryCmd.Flags().StringSliceVar(&metricsFormulas, "formulas", nil, "Formula expressions (e.g., \"anomalies(query0, \\\"basic\\\", 2)\")")
	metricsQueryCmd.Flags().IntVar(&metricsInterval, "interval", 0, "Time bucket interval in milliseconds")
	metricsQueryCmd.Flags().BoolVar(&metricsRaw, "raw", false, "Return raw CSV data instead of binned stats")
	metricsQueryCmd.Flags().BoolVar(&metricsCloudCost, "cloud-cost", false, "Query Cloud Cost Management data")
	metricsQueryCmd.MarkFlagRequired("queries")

	metricsListCmd.Flags().StringVar(&metricsNameFilter, "name-filter", "", "Substring/wildcard filter (e.g., \"system.cpu\")")
	metricsListCmd.Flags().StringVar(&metricsTagFilter, "tag-filter", "", "Tag filter (e.g., \"service:redis* AND host:prod-1\")")

	metricsContextCmd.Flags().BoolVar(&metricsCtxTags, "include-tags", false, "Include all tag values")
	metricsContextCmd.Flags().BoolVar(&metricsCtxAssets, "include-assets", false, "Include related dashboards/monitors/SLOs")
	metricsContextCmd.Flags().StringVar(&metricsCtxScope, "scope-tags", "", "Pre-filter tags (comma-separated, e.g., env:prod,region:eu)")

	metricsSubmitCmd.Flags().StringVar(&metricsSubmitName, "metric", "", "Metric name (required)")
	metricsSubmitCmd.Flags().Float64Var(&metricsSubmitVal, "value", 0, "Metric value (required)")
	metricsSubmitCmd.Flags().StringVar(&metricsSubmitTags, "tags", "", "Comma-separated tags (e.g., env:prod,service:web)")
	metricsSubmitCmd.MarkFlagRequired("metric")
}

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Query, list, and submit metrics",
}

var metricsQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query metrics with multi-query support and formulas",
	Long: `Query one or more metrics with optional formula expressions.

Examples:
  ddx metrics query --queries "avg:system.cpu.user{*}" --from 1h
  ddx metrics query --queries "avg:system.cpu.user{env:prod}","avg:system.cpu.system{env:prod}" --formulas "query0 + query1" --from 4h
  ddx metrics query --queries "avg:trace.servlet.request.hits{service:web}" --formulas 'anomalies(query0, "basic", 2)' --from 24h
  ddx metrics query --queries "avg:system.cpu.user{*} by {host}" --formulas 'top(query0, 10, "mean", "desc")'`,
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

		// Build query objects
		queries := make([]map[string]any, len(metricsQueries))
		for i, q := range metricsQueries {
			dataSource := "metrics"
			if metricsCloudCost {
				dataSource = "cloud_cost"
			}
			queries[i] = map[string]any{
				"name":        "query" + strconv.Itoa(i),
				"data_source": dataSource,
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

		attrs := body["data"].(map[string]any)["attributes"].(map[string]any)

		if len(metricsFormulas) > 0 {
			formulas := make([]map[string]any, len(metricsFormulas))
			for i, f := range metricsFormulas {
				formulas[i] = map[string]any{"formula": f}
			}
			attrs["formulas"] = formulas
		}

		if metricsInterval > 0 {
			attrs["interval"] = metricsInterval
		}

		data, err := c.Post(context.Background(), "api/v2/query/timeseries", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var metricsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available metrics",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if metricsNameFilter != "" {
			params.Set("filter[metric]", metricsNameFilter)
		}
		if metricsTagFilter != "" {
			params.Set("filter[tags]", metricsTagFilter)
		}
		params.Set("page[size]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/metrics", params)
		if err != nil {
			return err
		}

		return printData("metrics.list", extractData(data))
	},
}

var metricsMetadataCmd = &cobra.Command{
	Use:   "metadata <metric-name>",
	Short: "Get metric metadata (type, unit, description)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/metrics/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var metricsContextCmd = &cobra.Command{
	Use:   "context <metric-name>",
	Short: "Get metric context: tags, dimensions, related assets",
	Long: `Get detailed context for a metric including available tags and related assets.

Examples:
  ddx metrics context system.cpu.user
  ddx metrics context system.cpu.user --include-tags --include-assets
  ddx metrics context system.cpu.user --scope-tags "env:prod,region:eu"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		result := map[string]any{"metric": args[0]}

		// Fetch tags
		params := url.Values{}
		if metricsCtxScope != "" {
			for _, tag := range strings.Split(metricsCtxScope, ",") {
				params.Add("filter[tags]", strings.TrimSpace(tag))
			}
		}
		tags, err := c.Get(context.Background(), "api/v2/metrics/"+args[0]+"/all-tags", params)
		if err == nil {
			var tagData any
			json.Unmarshal(tags, &tagData)
			result["tags"] = tagData
		}

		// Fetch related assets if requested
		if metricsCtxAssets {
			assets, err := c.Get(context.Background(), "api/v2/metrics/"+args[0]+"/assets", nil)
			if err == nil {
				var assetData any
				json.Unmarshal(assets, &assetData)
				result["related_assets"] = assetData
			}
		}

		out, _ := json.Marshal(result)
		return printData("", out)
	},
}

var metricsSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a custom metric data point",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		var tags []string
		if metricsSubmitTags != "" {
			tags = strings.Split(metricsSubmitTags, ",")
		}

		body := map[string]any{
			"series": []map[string]any{
				{
					"metric": metricsSubmitName,
					"type":   0, // gauge
					"points": [][]any{
						{float64(0), metricsSubmitVal}, // timestamp 0 = now
					},
					"tags": tags,
				},
			},
		}

		data, err := c.Post(context.Background(), "api/v2/series", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
