package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

var (
	usagecostFamilies        string
	usagecostHourlyNext      string
	usagecostEstimatedMonth  string
	usagecostEstimatedView   string
	usagecostHistoricalMonth string
	usagecostHistoricalView  string
	usagecostProjectedView   string
	usagecostBillableMonth   string
	usagecostTopMetricsMonth string
	usagecostTopMetricsNames string
	usagecostLogsIndexNames  string
	usagecostBillingDimMonth string
	usagecostBillingDimView  string
)

func init() {
	rootCmd.AddCommand(usageCmd)
	usageCmd.AddCommand(usageSummaryCmd)
	usageCmd.AddCommand(usagecostHourlyCmd)
	usageCmd.AddCommand(usagecostEstimatedCmd)
	usageCmd.AddCommand(usagecostHistoricalCmd)
	usageCmd.AddCommand(usagecostProjectedCmd)
	usageCmd.AddCommand(usagecostBillableCmd)
	usageCmd.AddCommand(usagecostTopMetricsCmd)
	usageCmd.AddCommand(usagecostLogsByIndexCmd)
	usageCmd.AddCommand(usagecostBillingDimensionsCmd)

	usagecostHourlyCmd.Flags().StringVar(&usagecostFamilies, "families", "", `Comma-separated product families, e.g. "logs,apm" or "all" (required)`)
	usagecostHourlyCmd.MarkFlagRequired("families")
	usagecostHourlyCmd.Flags().StringVar(&usagecostHourlyNext, "next", "", "Resume from this pagination cursor (meta.pagination.next_record_id) instead of starting at the first page")

	usagecostEstimatedCmd.Flags().StringVar(&usagecostEstimatedMonth, "month", "", "ISO month YYYY-MM (or RFC3339); defaults to the current month")
	usagecostEstimatedCmd.Flags().StringVar(&usagecostEstimatedView, "view", "", "summary (default) or sub-org")

	usagecostHistoricalCmd.Flags().StringVar(&usagecostHistoricalMonth, "month", "", "ISO month YYYY-MM (or RFC3339) cost begins in (required)")
	usagecostHistoricalCmd.MarkFlagRequired("month")
	usagecostHistoricalCmd.Flags().StringVar(&usagecostHistoricalView, "view", "", "summary (default) or sub-org")

	usagecostProjectedCmd.Flags().StringVar(&usagecostProjectedView, "view", "", "summary (default) or sub-org")

	usagecostBillableCmd.Flags().StringVar(&usagecostBillableMonth, "month", "", "ISO month YYYY-MM (or RFC3339); defaults to the current month")

	usagecostTopMetricsCmd.Flags().StringVar(&usagecostTopMetricsMonth, "month", "", "ISO month YYYY-MM (or RFC3339); defaults to the current month")
	usagecostTopMetricsCmd.Flags().StringVar(&usagecostTopMetricsNames, "names", "", "Comma-separated metric names to filter to")

	usagecostLogsByIndexCmd.Flags().StringVar(&usagecostLogsIndexNames, "index-name", "", "Comma-separated log index names to filter to")

	usagecostBillingDimensionsCmd.Flags().StringVar(&usagecostBillingDimMonth, "month", "", "ISO month YYYY-MM (or RFC3339); defaults to the current month")
	usagecostBillingDimensionsCmd.Flags().StringVar(&usagecostBillingDimView, "view", "", "active (default) or all")
}

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Datadog usage metrics",
}

var usageSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Get usage summary",
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

		data, err := c.Get(context.Background(), "api/v1/usage/summary", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var usagecostHourlyCmd = &cobra.Command{
	Use:   "hourly",
	Short: "Get hourly usage by product family",
	Long: `Get hourly usage broken down by Datadog product family (api/v2/usage/hourly_usage).

Uses the global --from/--to for the timestamp window (hour precision,
YYYY-MM-DDThh). Auto-paginates via meta.pagination.next_record_id, capped at
10 pages of up to 500 records each.

Examples:
  ddx usage hourly --families logs --from 24h
  ddx usage hourly --families apm,rum --from 7d --to now
  ddx usage hourly --families all --from 30d --next <cursor>`,
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

		fromISO := usagecostHourISO(from)
		toISO := usagecostHourISO(to)
		first := true
		items, err := usagecostPaginateCursor(func(cursor string) ([]json.RawMessage, string, error) {
			effectiveCursor := cursor
			if first && usagecostHourlyNext != "" {
				effectiveCursor = usagecostHourlyNext
			}
			first = false

			params := usagecostBuildHourlyParams(usagecostFamilies, fromISO, toISO, effectiveCursor)
			data, err := c.Get(context.Background(), "api/v2/usage/hourly_usage", params)
			if err != nil {
				return nil, "", err
			}
			return usagecostExtractCursorPage(data)
		}, 10, func(time.Duration) {})
		if err != nil {
			return err
		}

		out, err := json.Marshal(items)
		if err != nil {
			return err
		}
		usagecostReportCount("usage hourly", len(items))
		return printData("usage.hourly", out)
	},
}

var usagecostEstimatedCmd = &cobra.Command{
	Use:   "estimated",
	Short: "Get estimated cost across your account",
	Long: `Get estimated cost across your account (api/v2/usage/estimated_cost).

Estimated cost is only available for the CURRENT and PREVIOUS month, and is
delayed up to 72 hours from when it was incurred. Live-probed on this org
2026-07-20: --month 2026-06 returned an empty {"data":[]} while the bare
default (current month) returned data. For anything older, use
"ddx usage historical" instead.

Examples:
  ddx usage estimated
  ddx usage estimated --month 2026-07
  ddx usage estimated --view sub-org`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		month, err := ucParseMonth(usagecostEstimatedMonth)
		if err != nil {
			return err
		}

		params := url.Values{}
		if month != "" {
			params.Set("start_month", month)
		}
		if usagecostEstimatedView != "" {
			params.Set("view", usagecostEstimatedView)
		}

		data, err := c.Get(context.Background(), "api/v2/usage/estimated_cost", params)
		if err != nil {
			return err
		}

		items := flattenV2Items(extractData(data))
		usagecostReportCount("usage estimated", usagecostCountItems(items))
		return printData("usage.estimated", items)
	},
}

var usagecostHistoricalCmd = &cobra.Command{
	Use:   "historical",
	Short: "Get historical cost across your account",
	Long: `Get historical cost across your account (api/v2/usage/historical_cost).

Cost data for a given month becomes available no later than the 16th of the
following month. Parent-level organizations only.

Examples:
  ddx usage historical --month 2026-05
  ddx usage historical --month 2026-01 --view sub-org`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		month, err := ucParseMonth(usagecostHistoricalMonth)
		if err != nil {
			return err
		}
		if month == "" {
			return fmt.Errorf("--month is required")
		}

		params := url.Values{}
		params.Set("start_month", month)
		if usagecostHistoricalView != "" {
			params.Set("view", usagecostHistoricalView)
		}

		data, err := c.Get(context.Background(), "api/v2/usage/historical_cost", params)
		if err != nil {
			return err
		}

		items := flattenV2Items(extractData(data))
		usagecostReportCount("usage historical", usagecostCountItems(items))
		return printData("usage.historical", items)
	},
}

var usagecostProjectedCmd = &cobra.Command{
	Use:   "projected",
	Short: "Get projected cost across your account",
	Long: `Get projected cost across your account (api/v2/usage/projected_cost).

Projected cost is only available for the current month, becoming available
around the 12th of the month — there is no month parameter, it always
reflects the current month. Live-probed 200 with no params on this org.

Examples:
  ddx usage projected
  ddx usage projected --view sub-org`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if usagecostProjectedView != "" {
			params.Set("view", usagecostProjectedView)
		}

		data, err := c.Get(context.Background(), "api/v2/usage/projected_cost", params)
		if err != nil {
			return err
		}

		items := flattenV2Items(extractData(data))
		usagecostReportCount("usage projected", usagecostCountItems(items))
		return printData("usage.projected", items)
	},
}

var usagecostBillableCmd = &cobra.Command{
	Use:   "billable",
	Short: "Get billable usage summary across your account",
	Long: `Get billable usage summary across your account (api/v1/usage/billable-summary).

Parent-level organizations only.

Examples:
  ddx usage billable
  ddx usage billable --month 2026-06`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		month, err := ucParseMonth(usagecostBillableMonth)
		if err != nil {
			return err
		}

		params := url.Values{}
		if month != "" {
			params.Set("month", month)
		}

		data, err := c.Get(context.Background(), "api/v1/usage/billable-summary", params)
		if err != nil {
			return err
		}

		items := usagecostUnwrapUsage(data)
		usagecostReportCount("usage billable", usagecostCountItems(items))
		return printData("usage.billable", items)
	},
}

var usagecostTopMetricsCmd = &cobra.Command{
	Use:   "top-metrics",
	Short: "Get custom metrics by hourly average",
	Long: `Get all custom metrics by hourly average (api/v1/usage/top_avg_metrics).

Uses the global --limit as the page size (API max 5000, default 500). Only a
single page is fetched — pass --limit higher if you need more than the
default page.

Examples:
  ddx usage top-metrics
  ddx usage top-metrics --month 2026-06 --names my.custom.metric,other.metric
  ddx usage top-metrics --limit 1000`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		month, err := ucParseMonth(usagecostTopMetricsMonth)
		if err != nil {
			return err
		}

		params := url.Values{}
		if month != "" {
			params.Set("month", month)
		}
		if usagecostTopMetricsNames != "" {
			params.Set("names", usagecostTopMetricsNames)
		}
		if limitFlag > 0 {
			params.Set("limit", strconv.Itoa(limitFlag))
		}

		data, err := c.Get(context.Background(), "api/v1/usage/top_avg_metrics", params)
		if err != nil {
			return err
		}

		items := usagecostUnwrapUsage(data)
		usagecostReportCount("usage top-metrics", usagecostCountItems(items))
		return printData("usage.top-metrics", items)
	},
}

var usagecostLogsByIndexCmd = &cobra.Command{
	Use:   "logs-by-index",
	Short: "Get hourly log usage by index",
	Long: `Get hourly usage for logs by index (api/v1/usage/logs_by_index).

Maps the global --from/--to to start_hr/end_hr, formatted to hour precision
(YYYY-MM-DDThh) per the API's required format — live-probed on this org.

Examples:
  ddx usage logs-by-index --from 24h
  ddx usage logs-by-index --from 7d --index-name main,pci`,
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
		params.Set("start_hr", usagecostHourISO(from))
		params.Set("end_hr", usagecostHourISO(to))
		if usagecostLogsIndexNames != "" {
			params.Set("index_name", usagecostLogsIndexNames)
		}

		data, err := c.Get(context.Background(), "api/v1/usage/logs_by_index", params)
		if err != nil {
			return err
		}

		items := usagecostUnwrapUsage(data)
		usagecostReportCount("usage logs-by-index", usagecostCountItems(items))
		return printData("usage.logs-by-index", items)
	},
}

var usagecostBillingDimensionsCmd = &cobra.Command{
	Use:   "billing-dimensions",
	Short: "Get billing dimension mapping for usage endpoints",
	Long: `Get a mapping of billing dimensions to the corresponding usage-endpoint
keys (api/v2/usage/billing_dimension_mapping). Parent-level organizations
only; mapping data updates on a monthly cadence.

Examples:
  ddx usage billing-dimensions
  ddx usage billing-dimensions --month 2026-06 --view all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		month, err := ucParseMonth(usagecostBillingDimMonth)
		if err != nil {
			return err
		}

		params := url.Values{}
		if month != "" {
			params.Set("filter[month]", month)
		}
		if usagecostBillingDimView != "" {
			params.Set("filter[view]", usagecostBillingDimView)
		}

		data, err := c.Get(context.Background(), "api/v2/usage/billing_dimension_mapping", params)
		if err != nil {
			return err
		}

		items := flattenV2Items(extractData(data))
		usagecostReportCount("usage billing-dimensions", usagecostCountItems(items))
		return printData("usage.billing-dimensions", items)
	},
}

// --- Pure helpers (shared with cost.go) ---

// ucParseMonth normalizes a month value into Datadog's ISO-8601 YYYY-MM
// format. Accepts a bare "YYYY-MM" or a full RFC3339 timestamp (only the
// year-month is kept). Empty input returns "" with no error — callers treat
// that as "omit the query param and let the API apply its own default".
func ucParseMonth(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.Format("2006-01"), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01"), nil
	}
	return "", fmt.Errorf("invalid month %q: expected YYYY-MM or RFC3339", s)
}

// usagecostHourISO formats a unix timestamp to the hour-precision ISO-8601
// format ([YYYY-MM-DDThh]) required by the hourly_usage/logs_by_index
// endpoints.
func usagecostHourISO(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02T15")
}

// usagecostBuildHourlyParams constructs the query params for
// api/v2/usage/hourly_usage. cursor is the page[next_record_id] to resume
// from ("" for the first page).
func usagecostBuildHourlyParams(families, fromISO, toISO, cursor string) url.Values {
	params := url.Values{}
	params.Set("filter[timestamp][start]", fromISO)
	params.Set("filter[timestamp][end]", toISO)
	params.Set("filter[product_families]", families)
	if cursor != "" {
		params.Set("page[next_record_id]", cursor)
	}
	return params
}

// usagecostExtractCursorPage unwraps a {"data": [...], "meta": {"pagination":
// {"next_record_id": ...}}} page — the shape shared by hourly_usage and
// monthly_cost_attribution — into flattened items (id merged into
// attributes via flattenV2Items) plus the next cursor ("" when exhausted).
func usagecostExtractCursorPage(raw json.RawMessage) ([]json.RawMessage, string, error) {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			Pagination struct {
				NextRecordID *string `json:"next_record_id"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, "", err
	}

	flat := flattenV2Items(wrapper.Data)
	var items []json.RawMessage
	if json.Unmarshal(flat, &items) != nil {
		// Not array-shaped — keep the flattened value as a single opaque
		// item rather than dropping it silently.
		if len(flat) > 0 && string(flat) != "null" {
			items = []json.RawMessage{flat}
		}
	}

	next := ""
	if wrapper.Meta.Pagination.NextRecordID != nil {
		next = *wrapper.Meta.Pagination.NextRecordID
	}
	return items, next, nil
}

// usagecostPaginateCursor drives a cursor-based pagination loop for
// next_record_id-style endpoints (hourly_usage, monthly_cost_attribution).
// fetch is called with the current cursor ("" on the first call) and must
// return that page's items plus the next cursor ("" when exhausted). The
// loop stops at maxPages (safety cap against a runaway/misbehaving API) or
// as soon as the cursor comes back empty or repeats. sleep is invoked
// between pages (never before the first) — callers pass time.Sleep to honor
// an endpoint's rate-limit guidance, or a no-op where none applies, so tests
// never actually block.
func usagecostPaginateCursor(fetch func(cursor string) ([]json.RawMessage, string, error), maxPages int, sleep func(time.Duration)) ([]json.RawMessage, error) {
	var all []json.RawMessage
	cursor := ""
	for page := 0; page < maxPages; page++ {
		items, next, err := fetch(cursor)
		if err != nil {
			return all, err
		}
		all = append(all, items...)
		if next == "" || next == cursor {
			break
		}
		sleep(5 * time.Second)
		cursor = next
	}
	return all, nil
}

// usagecostUnwrapUsage extracts the top-level "usage" array from a v1 Usage
// Metering response ({"usage": [...]}), returning the raw input unchanged
// when that key isn't present.
func usagecostUnwrapUsage(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Usage != nil {
		return wrapper.Usage
	}
	return raw
}

// usagecostFlattenV2Single flattens a v2 {"id":"...", "attributes":{...}}
// SINGLE object (not an array) by merging id into attributes — used by
// endpoints like active_billing_dimensions whose "data" is one object
// rather than a list.
func usagecostFlattenV2Single(data json.RawMessage) json.RawMessage {
	var obj struct {
		ID         string          `json:"id"`
		Attributes json.RawMessage `json:"attributes"`
	}
	if json.Unmarshal(data, &obj) != nil || obj.Attributes == nil {
		return data
	}
	var attrs map[string]any
	if json.Unmarshal(obj.Attributes, &attrs) != nil {
		return data
	}
	attrs["id"] = obj.ID
	out, err := json.Marshal(attrs)
	if err != nil {
		return data
	}
	return out
}

// usagecostCountItems returns the length of a JSON array, or 0 if data isn't
// array-shaped.
func usagecostCountItems(data json.RawMessage) int {
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return 0
	}
	return len(items)
}

// usagecostReportCount prints a "<cmd>: N records" line to stderr, mirroring
// extractWithMeta's count-line convention for endpoints whose pagination
// metadata doesn't carry a reusable total (next_record_id cursors and
// unpaginated v1 "usage" array shapes).
func usagecostReportCount(cmdName string, n int) {
	fmt.Fprintf(os.Stderr, "%s: %d records\n", cmdName, n)
}
