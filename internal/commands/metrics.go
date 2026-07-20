package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

var (
	metricsQueries    []string
	metricsFormulas   []string
	metricsInterval   string
	metricsRaw        bool
	metricsCloudCost  bool
	metricsSummary    bool
	metricsNameFilter string
	metricsTagFilter  string
	metricsSubmitName string
	metricsSubmitVal  float64
	metricsSubmitTags string
	metricsCtxTags    bool
	metricsCtxAssets  bool
	metricsCtxScope   string

	// metrics-ergo package additions — flags below are new surface added on
	// top of the pre-existing metrics command tree. See metricsergo* helpers
	// further down for the pure logic behind them.
	metricsergoMetricHint  string
	metricsergoStrictTypes bool
)

func init() {
	rootCmd.AddCommand(metricsCmd)
	metricsCmd.AddCommand(metricsQueryCmd)
	metricsCmd.AddCommand(metricsListCmd)
	metricsCmd.AddCommand(metricsMetadataCmd)
	metricsCmd.AddCommand(metricsContextCmd)
	metricsCmd.AddCommand(metricsSubmitCmd)

	metricsQueryCmd.Flags().StringArrayVar(&metricsQueries, "queries", nil, "Metric queries (repeatable, e.g. --queries \"avg:a{*}\" --queries \"avg:b{*}\"; a single value may also comma-join multiple queries — split happens outside {}/()/quotes, so \"avg:a{x:1,y:2},avg:b{*}\" works)")
	metricsQueryCmd.Flags().StringSliceVar(&metricsFormulas, "formulas", nil, "Formula expressions (e.g., \"anomalies(query0, \\\"basic\\\", 2)\")")
	metricsQueryCmd.Flags().StringVar(&metricsInterval, "interval", "", "Time bucket interval: milliseconds (e.g. 300000, back-compat), a Go duration (e.g. 90s, 1h30m), or day/week suffix (e.g. 1d, 2w)")
	metricsQueryCmd.Flags().BoolVar(&metricsRaw, "raw", false, "Return raw CSV data instead of binned stats")
	metricsQueryCmd.Flags().BoolVar(&metricsCloudCost, "cloud-cost", false, "Query Cloud Cost Management data")
	metricsQueryCmd.Flags().BoolVar(&metricsSummary, "summary", false, "Return binned stats (min/max/avg per time bucket)")
	metricsQueryCmd.Flags().StringVar(&metricsergoMetricHint, "metric", "", "Shortcut for a single query: builds \"avg:<metric>{*}\" (mutually exclusive with --queries)")
	metricsQueryCmd.Flags().BoolVar(&metricsergoStrictTypes, "strict-types", false, "Error instead of warn when a sum: query targets a gauge metric")

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
  ddx metrics query --queries "avg:system.cpu.user{env:prod}" --queries "avg:system.cpu.system{env:prod}" --formulas "query0 + query1" --from 4h
  ddx metrics query --queries "avg:trace.servlet.request.hits{service:web}" --formulas 'anomalies(query0, "basic", 2)' --from 24h
  ddx metrics query --queries "avg:system.cpu.user{*} by {host}" --formulas 'top(query0, 10, "mean", "desc")'
  ddx metrics query --queries "avg:a{x:1,y:2},avg:b{*}" --from 1h   # comma-joined, split outside {}
  ddx metrics query --metric system.cpu.user --from 1h             # shortcut for avg:system.cpu.user{*}
  ddx metrics query --queries "avg:system.cpu.user{*}" --interval 1h --from 24h
  ddx metrics query --queries "sum:system.disk.used{*}" --strict-types --from 1h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(metricsQueries) > 0 && metricsergoMetricHint != "" {
			return fmt.Errorf("--metric and --queries are mutually exclusive — drop --metric and express it as a --queries value instead")
		}
		if len(metricsQueries) == 0 && metricsergoMetricHint == "" {
			return fmt.Errorf("at least one of --queries or --metric is required")
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		var queryStrs []string
		if metricsergoMetricHint != "" {
			q := metricsergoBuildMetricQuery(metricsergoMetricHint)
			fmt.Fprintf(os.Stderr, "--metric interpreted as %s; use --queries for full control\n", q)
			queryStrs = []string{q}
		} else {
			queryStrs = splitQueriesTopLevel(metricsQueries)
		}

		if err := metricsergoCheckGaugeSums(c, queryStrs, metricsergoStrictTypes); err != nil {
			return err
		}

		intervalMillis, err := metricsergoParseIntervalMillis(metricsInterval)
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
		queries := make([]map[string]any, len(queryStrs))
		for i, q := range queryStrs {
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

		if intervalMillis > 0 {
			attrs["interval"] = intervalMillis
		}

		data, err := c.Post(context.Background(), "api/v2/query/timeseries", body)
		if err != nil {
			return err
		}

		if metricsSummary {
			data = computeMetricsSummary(data)
		}

		return printData("", data)
	},
}

func computeMetricsSummary(data json.RawMessage) json.RawMessage {
	// Datadog timeseries v2 response: attributes.times[], attributes.values[][], attributes.series[]
	var resp struct {
		Data struct {
			Attributes struct {
				Times  []int64     `json:"times"`
				Values [][]float64 `json:"values"`
				Series []struct {
					QueryIndex int      `json:"query_index"`
					GroupTags  []string `json:"group_tags"`
					Unit       []struct {
						Name string `json:"name"`
					} `json:"unit"`
				} `json:"series"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &resp) != nil {
		return data
	}

	attrs := resp.Data.Attributes
	var summaries []map[string]any

	for i, series := range attrs.Series {
		if i >= len(attrs.Values) {
			continue
		}
		vals := attrs.Values[i]
		if len(vals) == 0 {
			continue
		}

		min, max, sum := vals[0], vals[0], 0.0
		count := 0
		for _, v := range vals {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += v
			count++
		}

		s := map[string]any{
			"query_index": series.QueryIndex,
			"points":      count,
			"overall": map[string]any{
				"min": min,
				"max": max,
				"avg": sum / float64(count),
			},
		}
		if len(series.GroupTags) > 0 {
			s["group_tags"] = series.GroupTags
		}
		if len(series.Unit) > 0 && series.Unit[0].Name != "" {
			s["unit"] = series.Unit[0].Name
		}

		// Per-bucket breakdown
		var buckets []map[string]any
		for j, v := range vals {
			bucket := map[string]any{"value": v}
			if j < len(attrs.Times) {
				bucket["time"] = time.Unix(attrs.Times[j]/1000, 0).UTC().Format(time.RFC3339)
			}
			buckets = append(buckets, bucket)
		}
		s["buckets"] = buckets

		summaries = append(summaries, s)
	}

	out, _ := json.Marshal(summaries)
	return out
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
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would submit metric %q=%v, no data sent\n", metricsSubmitName, metricsSubmitVal)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("submitting metric %q", metricsSubmitName)); err != nil {
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

// --- metrics-ergo helpers -------------------------------------------------
//
// Pure parsing/extraction logic backing the query-ergonomics flags above
// (--metric, --interval, --strict-types) lives below, each covered by
// metrics_ergo_test.go.

var (
	// metricsergoAllDigitsRe matches an --interval value that is only digits,
	// preserving back-compat with the old IntVar-milliseconds flag.
	metricsergoAllDigitsRe = regexp.MustCompile(`^[0-9]+$`)

	// metricsergoDayWeekRe matches an --interval value using the "d"/"w"
	// suffixes that time.ParseDuration does not understand (e.g. "1d", "2w",
	// "1.5d").
	metricsergoDayWeekRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(d|w)$`)

	// metricsergoSumPrefixRe extracts the metric name from a query whose
	// aggregator is "sum:" — everything up to (but not including) the first
	// "{", so "by {tag}" clauses and scope filters are excluded.
	metricsergoSumPrefixRe = regexp.MustCompile(`^sum:([^{]+)`)
)

// metricsergoParseIntervalMillis parses the --interval flag into
// milliseconds. An all-digit string is treated as already-milliseconds
// (back-compat with the previous IntVar flag). Otherwise it accepts Go
// duration syntax (e.g. "90s", "1h30m") plus "d"/"w" suffixes for days/weeks
// that time.ParseDuration doesn't support. An empty string means "no
// interval" and returns 0, nil.
func metricsergoParseIntervalMillis(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	if metricsergoAllDigitsRe.MatchString(s) {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("invalid --interval %q: %w", s, err)
		}
		return n, nil
	}

	if d, err := time.ParseDuration(s); err == nil {
		return int(d.Milliseconds()), nil
	}

	if d, ok := metricsergoParseDayWeekDuration(s); ok {
		return int(d.Milliseconds()), nil
	}

	return 0, fmt.Errorf("invalid --interval %q: accepted forms are milliseconds (e.g. 300000), a Go duration (e.g. 90s, 1h30m), or a day/week suffix (e.g. 1d, 2w)", s)
}

// metricsergoParseDayWeekDuration parses a "<number>d" or "<number>w"
// duration string. It returns ok=false when s doesn't match that shape.
func metricsergoParseDayWeekDuration(s string) (time.Duration, bool) {
	m := metricsergoDayWeekRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	unit := 24 * time.Hour
	if m[2] == "w" {
		unit = 7 * 24 * time.Hour
	}
	return time.Duration(num * float64(unit)), true
}

// metricsergoBuildMetricQuery builds the shorthand query used by --metric.
func metricsergoBuildMetricQuery(metric string) string {
	return "avg:" + metric + "{*}"
}

// metricsergoSumMetricName extracts the metric name from a query whose
// aggregator is "sum:" (e.g. "sum:system.disk.used{*} by {host}" ->
// "system.disk.used"). ok is false when the query's aggregator isn't
// "sum:", or when "sum:" is followed by nothing but a "{" (no metric name).
func metricsergoSumMetricName(query string) (name string, ok bool) {
	m := metricsergoSumPrefixRe.FindStringSubmatch(query)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// metricsergoCheckGaugeSums warns (or, with strict=true, errors) when a
// query's aggregator is sum: applied to a gauge metric: sum: performs a
// temporal rollup that inflates gauge values (a real incident reported 86.8
// cores vs ~1.5 actual from this exact mistake). Metadata lookups are
// best-effort — a failed lookup is silently ignored so a metadata hiccup
// never blocks the underlying query.
func metricsergoCheckGaugeSums(c *client.Client, queries []string, strict bool) error {
	for _, q := range queries {
		name, ok := metricsergoSumMetricName(q)
		if !ok || name == "" {
			continue
		}

		data, err := c.Get(context.Background(), "api/v1/metrics/"+name, nil)
		if err != nil {
			continue
		}
		var meta struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		if meta.Type != "gauge" {
			continue
		}

		msg := fmt.Sprintf("%s: sum: on a gauge applies a sum temporal rollup and inflates results - use avg:/max:, or as_count() on counters", name)
		if strict {
			return fmt.Errorf("%s", msg)
		}
		fmt.Fprintln(os.Stderr, "warning: "+msg)
	}
	return nil
}
