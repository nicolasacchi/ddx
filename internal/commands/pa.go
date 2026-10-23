package commands

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	cliout "github.com/nicolasacchi/clicore/output"
	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/nicolasacchi/ddx/internal/output"
	"github.com/nicolasacchi/ddx/internal/ratelimit"
	"github.com/spf13/cobra"
)

// pa — Datadog Product Analytics analytics API (api/v2/product-analytics/analytics/*).
// Unlike api/v2/rum/events (which only sees the retention-filtered ~5% sample)
// this reads the UNSAMPLED event store: 15-month retention, raw event grain.
//
// All calls share one 60 req/min per-user server budget (x-ratelimit-name:
// analytics_extended_per_user), so every request goes through the cross-process
// limiter in internal/ratelimit — parallel ddx processes/agents coordinate.
//
// Package-prefixed identifiers (pa*) per namespace discipline.
var (
	paQueryFlag       string
	paColumnsFlag     string
	paSortFlag        string
	paPageSizeFlag    int
	paMaxRowsFlag     int
	paDataSourceFlag  string
	paFormatFlag      string
	paOutFlag         string
	paComputeFlag     string
	paGroupByFlag     []string
	paIntervalFlag    string
	paMinIntervalFlag time.Duration
	paCacheBustFlag   bool
)

const (
	paListPath       = "api/v2/product-analytics/analytics/list"
	paScalarPath     = "api/v2/product-analytics/analytics/scalar"
	paTimeseriesPath = "api/v2/product-analytics/analytics/timeseries"

	paMaxPageSize     = 1000
	paDefaultGroupLim = 20
	paRunningRetries  = 5
)

// paRunningDelay is how long to wait before re-issuing a query whose
// meta.status is "running" (var so tests can shrink it).
var paRunningDelay = 2 * time.Second

func init() {
	rootCmd.AddCommand(paCmd)
	paCmd.AddCommand(paListCmd)
	paCmd.AddCommand(paScalarCmd)
	paCmd.AddCommand(paTimeseriesCmd)

	paCmd.PersistentFlags().StringVar(&paDataSourceFlag, "data-source", "product_analytics", "Data source: product_analytics or product_analytics_occurrence")
	paCmd.PersistentFlags().StringVar(&paQueryFlag, "query", "*", `Product Analytics search query, e.g. "@type:action @action.name:\"Add to cart\""`)
	paCmd.PersistentFlags().BoolVar(&paCacheBustFlag, "cache-bust", true, "Make every request unique so the API's response cache (keyed on query shape + minute-rounded window) cannot serve stale data")
	paCmd.PersistentFlags().DurationVar(&paMinIntervalFlag, "min-interval", 0, "Minimum spacing between API calls across ALL ddx processes (default 1s = 60/min; env DDX_PA_MIN_INTERVAL)")

	paListCmd.Flags().StringVar(&paColumnsFlag, "columns", "", "Comma-separated columns to return, e.g. @action.name,@session.id,@view.name (required; timestamp is always included)")
	paListCmd.Flags().StringVar(&paSortFlag, "sort", "desc", "Timestamp order: asc or desc")
	paListCmd.Flags().IntVar(&paPageSizeFlag, "page-size", paMaxPageSize, "Rows per API call (1-1000)")
	paListCmd.Flags().IntVar(&paMaxRowsFlag, "max-rows", 0, "Stop after N rows (0 = all; under CLAUDECODE the default is a 100-row cap unless set explicitly or --out is used)")
	paListCmd.Flags().StringVar(&paFormatFlag, "format", "json", "Output format: json (array), jsonl, csv")
	paListCmd.Flags().StringVar(&paOutFlag, "out", "", "Write rows to this file instead of stdout (never agent-capped)")
	if err := paListCmd.MarkFlagRequired("columns"); err != nil {
		panic(err)
	}

	for _, c := range []*cobra.Command{paScalarCmd, paTimeseriesCmd} {
		c.Flags().StringVar(&paComputeFlag, "compute", "count", `Aggregation: count | cardinality(@f) | sum(@f) | avg(@f) | min(@f) | max(@f)`)
		c.Flags().StringArrayVar(&paGroupByFlag, "group-by", nil, `Group by "@facet[:limit]" (limit default 20), repeatable`)
		c.Flags().StringVar(&paFormatFlag, "format", "json", "Output format: json, csv")
	}
	paTimeseriesCmd.Flags().StringVar(&paIntervalFlag, "interval", "1h", "Bucket size: Go duration (30m, 1h) or day/week suffix (1d, 1w)")
}

var paCmd = &cobra.Command{
	Use:     "pa",
	Aliases: []string{"product-analytics"},
	Short:   "Product Analytics — unsampled event list, scalar and timeseries analytics",
	Long: `Query the Datadog Product Analytics analytics API: the UNSAMPLED event store
(15-month retention, raw event grain), unlike 'ddx rum events' which only sees
the ~5% retention-filtered sample.

All subcommands share ONE server budget of 60 requests/minute per user. ddx
paces calls through a cross-process limiter (state under
$XDG_STATE_HOME/ddx/ratelimit/), so any number of parallel ddx processes using
the same keys stay under the limit together. It also honours x-ratelimit-*
headers and retries 429s/5xx. Tune with --min-interval or DDX_PA_MIN_INTERVAL.

Examples:
  ddx pa list --query "@type:action" --columns @action.name,@session.id --from 1d --format csv --out actions.csv
  ddx pa scalar --query "@type:action" --compute count --group-by @type --from 7d
  ddx pa timeseries --query "@type:view" --compute "cardinality(@session.id)" --interval 1d --from 14d`,
}

var paListCmd = &cobra.Command{
	Use:   "list",
	Short: "Export raw events (keyset-paged, streamed, boundary-safe)",
	Long: `Export raw Product Analytics events in the window [--from, --to).

Rows are streamed as they arrive (jsonl/csv/json), so 100k+ row pulls do not
buffer in memory. Progress goes to stderr; at the end ddx prints the number of
rows fetched against the API's total_count and says so if they differ.

Paging caveats (why this is not offset paging):
  - The API caps a page at 1000 rows and has NO cursor/offset, so ddx pages by
    keyset on "timestamp". Timestamps are not unique, and --from/--to are
    FLOORED to whole seconds by the server (a sub-second window is an API
    error), so the paging unit is the second.
  - desc: next page = [from, end of the oldest row's second); asc: next page =
    [start of the newest row's second, to). The boundary second is therefore
    fetched twice and the repeats are removed by exact whole-record match
    (multiplicity-preserving, so genuinely identical events are kept).
  - If >= page-size rows fall inside ONE second the window cannot advance; ddx
    fails loudly (it never loops forever or drops rows). Use a narrower query.
  - Include enough columns to tell records apart; the dedupe key is the whole
    returned record (requested columns + timestamp). Columns that do not
    distinguish events make "rows" mean "distinct records under those columns".
  - Rows up to 999 ms before --from / after --to may appear: the API floors the
    window edges to whole seconds and ddx reports what it returns.
  - A window shorter than one whole second (or from >= to) is rejected.
  - The API caches responses by query shape + MINUTE-rounded from/to, so the
    same shape in the same minutes returns a stale, different-window result
    (seen live, for minutes). ddx therefore appends a harmless nonce term
    ("(Q) -@ddx_nonce:<hex>") to every request; --cache-bust=false disables it.

Output cap: this is an explicit export, so the global --limit does not apply.
Under CLAUDECODE (agent mode) stdout is capped at 100 rows unless you pass
--max-rows explicitly (0 = all) or write to a file with --out. A cap is always
reported on stderr; ddx never truncates silently.

Examples:
  ddx pa list --query "@type:action @action.name:\"Add to cart\"" --columns @session.id,@view.name --from 2d --format jsonl
  ddx pa list --query "@type:action" --columns @action.name --from 7d --sort asc --format csv --out week.csv
  ddx pa list --query "@type:error" --columns @error.message --max-rows 500 --data-source product_analytics`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jqFlag != "" {
			return fmt.Errorf("--jq is not supported by 'pa list' (it streams rows); pipe --format jsonl to jq instead")
		}
		columns := paSplitColumns(paColumnsFlag)
		if len(columns) == 0 {
			return fmt.Errorf("--columns is required (e.g. --columns @action.name,@session.id)")
		}
		if paSortFlag != "asc" && paSortFlag != "desc" {
			return fmt.Errorf("invalid --sort %q: must be asc or desc", paSortFlag)
		}
		if paPageSizeFlag < 1 || paPageSizeFlag > paMaxPageSize {
			return fmt.Errorf("invalid --page-size %d: must be 1-%d (API limit)", paPageSizeFlag, paMaxPageSize)
		}
		if paMaxRowsFlag < 0 {
			return fmt.Errorf("invalid --max-rows %d: must be >= 0", paMaxRowsFlag)
		}
		if err := paValidateDataSource(paDataSourceFlag); err != nil {
			return err
		}
		if paFormatFlag != "json" && paFormatFlag != "jsonl" && paFormatFlag != "csv" {
			return fmt.Errorf("invalid --format %q: must be json, jsonl or csv", paFormatFlag)
		}

		fromS, err := parseFrom()
		if err != nil {
			return err
		}
		toS, err := parseTo()
		if err != nil {
			return err
		}
		if err := paCheckWindow(fromS, toS); err != nil {
			return err
		}
		from, to := fromS*1000, toS*1000

		maxRows := paMaxRowsFlag
		capNote := ""
		if !cmd.Flags().Changed("max-rows") && paOutFlag == "" && cliout.AgentMode() {
			maxRows = cliout.AgentRowCap
			capNote = fmt.Sprintf("note: output capped to %d rows (CLAUDECODE); pass --max-rows 0 for all, or --out FILE", maxRows)
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		paWireNotify(c)
		rl, err := paNewLimiter()
		if err != nil {
			return err
		}

		var w io.Writer = os.Stdout
		var outFile *os.File
		if paOutFlag != "" {
			outFile, err = os.Create(paOutFlag)
			if err != nil {
				return fmt.Errorf("open --out: %w", err)
			}
			w = outFile
		}
		rw, err := paNewRowWriter(paFormatFlag, w, columns)
		if err != nil {
			if outFile != nil {
				outFile.Close()
			}
			return err
		}

		// Ctrl-C / SIGTERM cancel the walk; the writer is still closed below
		// (JSON array terminated, CSV flushed) and the summary reports INCOMPLETE.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		fetch := func(ctx context.Context, f, t int64) (paPage, error) {
			return paFetchList(ctx, c, rl, paListSpec{
				DataSource: paDataSourceFlag, Query: paQueryFlag, Columns: columns,
				Limit: paPageSizeFlag, Desc: paSortFlag == "desc", From: f, To: t,
				Bust: paCacheBustFlag,
			})
		}
		stats, perr := paPaginate(ctx, paPagerOpts{
			From: from, To: to, Desc: paSortFlag == "desc", PageSize: paPageSizeFlag, MaxRows: maxRows,
			Fetch: fetch, Emit: rw.Write,
			Progress: func(s paStats) {
				if err := rw.Flush(); err != nil && !quietFlag {
					fmt.Fprintf(os.Stderr, "pa list: flush: %v\n", err)
				}
				if !quietFlag {
					fmt.Fprintf(os.Stderr, "pa list: page %d, %d rows (api total_count %d)\n", s.Pages, s.Rows, s.Total)
				}
			},
		})
		if cerr := rw.Close(); cerr != nil && perr == nil {
			perr = cerr
		}
		if outFile != nil {
			// Explicit close: a deferred one would hide write errors (full disk).
			if cerr := outFile.Close(); cerr != nil && perr == nil {
				perr = fmt.Errorf("write --out: %w", cerr)
			}
			if perr != nil {
				fmt.Fprintf(os.Stderr, "pa list: --out %s is incomplete (%d rows written)\n", paOutFlag, stats.Rows)
			}
		}
		paReportSummary(stats, perr)
		if perr == nil && stats.Truncated {
			if capNote != "" {
				fmt.Fprintln(os.Stderr, capNote)
			}
			fmt.Fprintf(os.Stderr, "truncated: stopped at --max-rows after %d rows; more rows may exist (api total_count %d)\n", stats.Rows, stats.Total)
		}
		return perr
	},
}

var paScalarCmd = &cobra.Command{
	Use:   "scalar",
	Short: "One aggregated number per group (count, cardinality, sum, avg, min, max)",
	Long: `Aggregate Product Analytics events into one value per group.

Examples:
  ddx pa scalar --query "@type:action" --compute count --group-by @type --from 7d
  ddx pa scalar --query "@type:view" --compute "cardinality(@session.id)" --from 1d
  ddx pa scalar --query "@type:action" --compute "sum(@action.target.count)" --group-by "@view.name:50" --format csv`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return paRunAnalytics(cmd, paScalarPath, false)
	},
}

var paTimeseriesCmd = &cobra.Command{
	Use:   "timeseries",
	Short: "Aggregated values per time bucket (and group)",
	Long: `Aggregate Product Analytics events per time bucket. Rows are
(time, time_ms, <group facets...>, value).

Examples:
  ddx pa timeseries --query "@type:action" --interval 1h --from 24h
  ddx pa timeseries --query "@type:view" --compute "cardinality(@session.id)" --interval 1d --from 14d --group-by @device.type --format csv`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return paRunAnalytics(cmd, paTimeseriesPath, true)
	},
}

// paCheckWindow rejects windows the API cannot serve. from/to arrive as whole
// seconds (the API floors them anyway); an empty or inverted window would
// otherwise "succeed" with zero rows - indistinguishable from real absence.
func paCheckWindow(fromS, toS int64) error {
	if fromS > toS {
		return fmt.Errorf("--from is after --to (%s > %s)", time.Unix(fromS, 0).UTC().Format(time.RFC3339), time.Unix(toS, 0).UTC().Format(time.RFC3339))
	}
	if fromS == toS {
		return fmt.Errorf("window must span at least one whole second; the API floors --from/--to to seconds (both are %s)", time.Unix(fromS, 0).UTC().Format(time.RFC3339))
	}
	return nil
}

// paWireNotify prints a stderr note when a retry backoff exceeds 2s (unless --quiet).
func paWireNotify(c *client.Client) {
	c.SetRetryNotify(func(d time.Duration, why string) {
		if !quietFlag {
			fmt.Fprintf(os.Stderr, "pa: %s, retrying in %s\n", why, d.Round(100*time.Millisecond))
		}
	})
}

func paNonceIf(on bool) string {
	if !on {
		return ""
	}
	return paNonce()
}

func paRunAnalytics(cmd *cobra.Command, path string, timeseries bool) error {
	if paFormatFlag != "json" && paFormatFlag != "csv" {
		return fmt.Errorf("invalid --format %q: must be json or csv", paFormatFlag)
	}
	if err := paValidateDataSource(paDataSourceFlag); err != nil {
		return err
	}
	agg, metric, err := paParseCompute(paComputeFlag)
	if err != nil {
		return err
	}
	groups, err := paParseGroupBys(paGroupByFlag)
	if err != nil {
		return err
	}
	var intervalMs int64
	if timeseries {
		d, err := paParseInterval(paIntervalFlag)
		if err != nil {
			return err
		}
		intervalMs = d.Milliseconds()
	}
	fromS, err := parseFrom()
	if err != nil {
		return err
	}
	toS, err := parseTo()
	if err != nil {
		return err
	}
	if err := paCheckWindow(fromS, toS); err != nil {
		return err
	}
	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	paWireNotify(c)
	rl, err := paNewLimiter()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	raw, err := paPost(ctx, c, rl, path, func() any {
		return paAnalyticsBody(paDataSourceFlag, paQueryFlag, paCacheBustFlag, agg, metric, groups, intervalMs, fromS*1000, toS*1000)
	})
	if err != nil {
		return err
	}
	var rows []map[string]any
	if timeseries {
		rows, err = paTimeseriesRows(raw)
	} else {
		rows, err = paScalarRows(raw)
	}
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	if paFormatFlag == "csv" {
		return paWriteRowsCSV(os.Stdout, rows)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	// Aggregations are bounded by group limits / bucket count, so the agent
	// row cap (printData) is deliberately not applied.
	return output.PrintData("pa."+path[strings.LastIndex(path, "/")+1:], out, isJSONMode(), jqFlag)
}

// ---------------------------------------------------------------- limiter

func paNewLimiter() (*ratelimit.Limiter, error) {
	interval := paMinIntervalFlag
	if interval == 0 {
		var err error
		interval, err = ratelimit.IntervalFromEnv("DDX_PA_MIN_INTERVAL", ratelimit.DefaultPAInterval)
		if err != nil {
			return nil, err
		}
	}
	rl := ratelimit.New(ratelimit.PABucket, interval)
	rl.Notify = func(wait time.Duration) {
		if !quietFlag {
			fmt.Fprintf(os.Stderr, "pa: waiting %s for the shared Product Analytics rate limit (60 req/min per user)\n", wait.Round(100*time.Millisecond))
		}
	}
	return rl, nil
}

// ---------------------------------------------------------------- requests

func paValidateDataSource(ds string) error {
	if ds != "product_analytics" && ds != "product_analytics_occurrence" {
		return fmt.Errorf("invalid --data-source %q: must be product_analytics or product_analytics_occurrence", ds)
	}
	return nil
}

func paSplitColumns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// paNonce returns a fresh token for cache busting (var so tests can pin it).
var paNonce = func() string {
	var b [6]byte
	_, _ = crand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// paBustQuery makes a request unique. The Product Analytics API caches
// responses keyed on the query shape plus the from/to rounded to the MINUTE
// (verified live: same shape + same minutes => the first response comes back,
// for minutes, even for a different window). Appending a negated match on a
// nonexistent facet changes the shape without changing the result set.
func paBustQuery(query, nonce string) string {
	if nonce == "" {
		return query
	}
	return "(" + query + ") -@ddx_nonce:" + nonce
}

type paListSpec struct {
	DataSource string
	Query      string
	Bust       bool // make the request unique (see paBustQuery)
	Columns    []string
	Limit      int
	Desc       bool
	From, To   int64 // epoch ms; To exclusive
}

// paListBody builds the analytics/list request. sort is an OBJECT (a string 400s).
func paListBody(s paListSpec) map[string]any {
	order := "asc"
	if s.Desc {
		order = "desc"
	}
	return map[string]any{"data": map[string]any{
		"type": "formula_analytics_extended_list_request",
		"attributes": map[string]any{
			"from": s.From,
			"to":   s.To,
			"query": map[string]any{
				"query": map[string]any{
					"data_source": s.DataSource,
					"search":      map[string]any{"query": paBustQuery(s.Query, paNonceIf(s.Bust))},
				},
				"columns": s.Columns,
				"limit":   s.Limit,
				"sort":    map[string]any{"facet": "timestamp", "order": order},
			},
		},
	}}
}

type paGroup struct {
	Facet string
	Limit int
}

// paAnalyticsBody builds a scalar (intervalMs == 0) or timeseries request.
// "compute" is a SIBLING of the inner "query" (the API rejects it otherwise).
func paAnalyticsBody(dataSource, query string, bust bool, agg, metric string, groups []paGroup, intervalMs, from, to int64) map[string]any {
	compute := map[string]any{"aggregation": agg}
	if metric != "" {
		compute["metric"] = metric
	}
	q := map[string]any{
		"query": map[string]any{
			"data_source": dataSource,
			"search":      map[string]any{"query": paBustQuery(query, paNonceIf(bust))},
		},
		"compute": compute,
	}
	if len(groups) > 0 {
		gb := make([]map[string]any, len(groups))
		for i, g := range groups {
			gb[i] = map[string]any{"facet": g.Facet, "limit": g.Limit}
		}
		q["group_by"] = gb
	}
	typ := "formula_analytics_extended_request"
	if intervalMs > 0 {
		compute["interval"] = intervalMs
	}
	return map[string]any{"data": map[string]any{
		"type": typ,
		"attributes": map[string]any{
			"from":  from,
			"to":    to,
			"query": q,
		},
	}}
}

func paParseCompute(s string) (agg, metric string, err error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "count" {
		return "count", "", nil
	}
	open := strings.Index(s, "(")
	if open < 1 || !strings.HasSuffix(s, ")") {
		return "", "", fmt.Errorf("invalid --compute %q: use count or agg(@field), agg in cardinality|sum|avg|min|max", s)
	}
	agg, metric = s[:open], strings.TrimSpace(s[open+1:len(s)-1])
	switch agg {
	case "cardinality", "sum", "avg", "min", "max":
	default:
		return "", "", fmt.Errorf("invalid --compute aggregation %q: must be count, cardinality, sum, avg, min or max", agg)
	}
	if metric == "" {
		return "", "", fmt.Errorf("invalid --compute %q: %s needs a field, e.g. %s(@session.id)", s, agg, agg)
	}
	return agg, metric, nil
}

func paParseGroupBys(specs []string) ([]paGroup, error) {
	var out []paGroup
	for _, s := range specs {
		g := paGroup{Facet: strings.TrimSpace(s), Limit: paDefaultGroupLim}
		if i := strings.LastIndex(s, ":"); i > 0 {
			if n, err := strconv.Atoi(s[i+1:]); err == nil {
				if n < 1 {
					return nil, fmt.Errorf("invalid --group-by %q: limit must be >= 1", s)
				}
				g.Facet, g.Limit = strings.TrimSpace(s[:i]), n
			}
		}
		if g.Facet == "" {
			return nil, fmt.Errorf("invalid --group-by %q: expected @facet[:limit]", s)
		}
		out = append(out, g)
	}
	return out, nil
}

// paParseInterval accepts Go durations plus d/w suffixes (1d, 2w).
func paParseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n := len(s); n > 1 && (s[n-1] == 'd' || s[n-1] == 'w') {
		if v, err := strconv.Atoi(s[:n-1]); err == nil && v > 0 {
			unit := 24 * time.Hour
			if s[n-1] == 'w' {
				unit = 7 * 24 * time.Hour
			}
			return time.Duration(v) * unit, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("invalid --interval %q: use a duration >= 1s like 30m, 1h, 1d, 1w", s)
	}
	return d, nil
}

// paPost POSTs through the shared limiter and enforces meta.status: "done"
// (or absent) passes; "running" is re-issued a few times; "timeout" (or still
// running afterwards) fails — partial data is never returned as complete.
func paPost(ctx context.Context, c *client.Client, rl *ratelimit.Limiter, path string, mkBody func() any) (json.RawMessage, error) {
	var lastStatus string
	for try := 0; try <= paRunningRetries; try++ {
		raw, err := c.PostRateLimited(ctx, path, mkBody(), rl) // rebuilt per try: a fresh nonce also dodges a cached "running"
		if err != nil {
			return nil, err
		}
		var meta struct {
			Meta struct {
				Status    string `json:"status"`
				RequestID string `json:"request_id"`
			} `json:"meta"`
		}
		_ = json.Unmarshal(raw, &meta)
		switch meta.Meta.Status {
		case "", "done":
			return raw, nil
		case "running":
			lastStatus = "running"
			select {
			case <-time.After(paRunningDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return nil, &client.APIError{Kind: "async_timeout", Detail: fmt.Sprintf("query ended with status %q (request %s)", meta.Meta.Status, meta.Meta.RequestID), Hint: "narrow --from/--to or the query and retry"}
		}
	}
	return nil, &client.APIError{Kind: "async_timeout", Detail: "query still " + lastStatus + " after " + strconv.Itoa(paRunningRetries) + " retries", Hint: "narrow --from/--to or the query and retry"}
}

type paPage struct {
	Records []map[string]any
	Total   int
}

// paFetchList issues one analytics/list call and decodes the records
// (numbers kept as json.Number so large ints survive the round trip).
func paFetchList(ctx context.Context, c *client.Client, rl *ratelimit.Limiter, s paListSpec) (paPage, error) {
	raw, err := paPost(ctx, c, rl, paListPath, func() any { return paListBody(s) })
	if err != nil {
		return paPage{}, err
	}
	var resp struct {
		Data struct {
			Attributes struct {
				Records    []map[string]any `json:"records"`
				TotalCount int              `json:"total_count"`
			} `json:"attributes"`
		} `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&resp); err != nil {
		return paPage{}, fmt.Errorf("decode pa list response: %w", err)
	}
	return paPage{Records: resp.Data.Attributes.Records, Total: resp.Data.Attributes.TotalCount}, nil
}

// ---------------------------------------------------------------- keyset paging

type paStats struct {
	Pages     int
	Rows      int // rows emitted
	Dupes     int // boundary repeats removed
	Total     int // API total_count of the window (first page)
	Truncated bool
}

type paPagerOpts struct {
	From, To int64 // epoch ms, To exclusive
	Desc     bool
	PageSize int
	MaxRows  int // 0 = unlimited
	Fetch    func(ctx context.Context, from, to int64) (paPage, error)
	Emit     func(rec map[string]any) error
	Progress func(paStats)
}

// paSecond is the API's real time granularity: from/to are FLOORED to whole
// seconds server-side (verified live: [10500,12500) behaves as [10000,12000);
// a window inside one second fails with a 500), so the "to is exclusive in ms"
// contract does not hold below 1s and boundary paging must work on seconds.
const paSecond = int64(1000)

func paFloorSec(ms int64) int64 { return ms - ((ms%paSecond)+paSecond)%paSecond }

// paPaginate walks [From,To) in pages of PageSize using keyset paging on the
// "timestamp" (the API has no cursor/offset). Window edges are whole seconds,
// so the boundary unit is the SECOND containing the page's last timestamp:
//
//	desc: next window is [from, floor_s(minTs)+1s)  -> boundary second re-fetched
//	asc:  next window is [floor_s(maxTs), to)       -> boundary second re-fetched
//
// Re-fetched boundary-second rows are dropped by exact whole-record match
// against what was already emitted in that second (a multiset, so truly
// identical events are kept). A short page ends the walk. If the window cannot
// advance (>= PageSize rows inside a single second) it fails instead of
// looping or dropping rows. Every page is also checked to lie inside the
// requested window, so a stale/misbehaving response fails loudly.
func paPaginate(ctx context.Context, o paPagerOpts) (paStats, error) {
	var st paStats
	from, to := paFloorSec(o.From), paFloorSec(o.To)
	var boundary int64 = -1 // start (ms) of the boundary second, -1 = none
	var seen map[string]int // already-emitted records inside that second
	for from < to {
		page, err := o.Fetch(ctx, from, to)
		if err != nil {
			return st, err
		}
		st.Pages++
		if st.Pages == 1 {
			st.Total = page.Total
		}
		recs := page.Records
		if len(recs) == 0 {
			if o.Progress != nil {
				o.Progress(st)
			}
			break
		}

		minTs, maxTs := int64(1<<62), int64(-1)
		tss := make([]int64, len(recs))
		for i, r := range recs {
			ts, err := paTimestamp(r)
			if err != nil {
				return st, err
			}
			if ts < from || ts >= to {
				return st, fmt.Errorf("response row timestamp %d is outside the requested window [%d,%d) - the API returned data for a different window (stale cache?); retry with --cache-bust", ts, from, to)
			}
			tss[i] = ts
			minTs, maxTs = min(minTs, ts), max(maxTs, ts)
		}

		remaining := seen // consumed as repeats are matched
		stopped := false
		for i, r := range recs {
			if remaining != nil && paFloorSec(tss[i]) == boundary {
				k := paRecordKey(r)
				if remaining[k] > 0 {
					remaining[k]--
					st.Dupes++
					continue
				}
			}
			if o.MaxRows > 0 && st.Rows >= o.MaxRows {
				st.Truncated = true
				stopped = true
				break
			}
			if err := o.Emit(r); err != nil {
				return st, err
			}
			st.Rows++
		}
		if o.Progress != nil {
			o.Progress(st)
		}
		if stopped {
			return st, nil
		}
		if o.MaxRows > 0 && st.Rows >= o.MaxRows {
			// Cap reached exactly on a page edge: more exists iff the page was full.
			st.Truncated = len(recs) >= o.PageSize
			return st, nil
		}
		if len(recs) < o.PageSize {
			return st, nil
		}

		// Advance the window to the boundary second; remember what was emitted in it.
		if o.Desc {
			boundary = paFloorSec(minTs)
			if boundary+paSecond >= to {
				return st, paNoProgress(len(recs), boundary)
			}
			to = boundary + paSecond
		} else {
			boundary = paFloorSec(maxTs)
			if boundary <= from {
				return st, paNoProgress(len(recs), boundary)
			}
			from = boundary
		}
		seen = map[string]int{}
		for i, r := range recs {
			if paFloorSec(tss[i]) == boundary {
				seen[paRecordKey(r)]++
			}
		}
	}
	return st, nil
}

func paNoProgress(n int, ts int64) error {
	return fmt.Errorf("cannot page: a full page of %d rows lies inside the single second starting %s; the API has no cursor and only 1s window granularity, so this window cannot advance - narrow the query (e.g. add a filter) or use a smaller time range per query", n, time.UnixMilli(ts).UTC().Format(time.RFC3339))
}

func paTimestamp(r map[string]any) (int64, error) {
	switch v := r["timestamp"].(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, nil
		}
		if f, err := v.Float64(); err == nil {
			return int64(f), nil
		}
	case float64:
		return int64(v), nil
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	}
	return 0, fmt.Errorf("record has no numeric \"timestamp\" field, cannot page: %v", r)
}

// paRecordKey is the canonical JSON of the whole record (encoding/json sorts map keys).
func paRecordKey(r map[string]any) string {
	b, _ := json.Marshal(r)
	return string(b)
}

func paReportSummary(st paStats, err error) {
	if !quietFlag || err != nil {
		fmt.Fprintf(os.Stderr, "pa list: fetched %d rows in %d pages (api total_count %d, %d boundary repeats removed)\n", st.Rows, st.Pages, st.Total, st.Dupes)
	}
	// The mismatch note is a data-integrity warning, not progress: never silenced by --quiet.
	if err == nil && !st.Truncated && st.Rows != st.Total {
		fmt.Fprintf(os.Stderr, "note: rows fetched (%d) differ from api total_count (%d) by %+d - the count can drift while paging live data; small gaps are expected, large ones are not\n", st.Rows, st.Total, st.Rows-st.Total)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pa list: INCOMPLETE - aborted after %d rows: %v\n", st.Rows, err)
	}
}

// ---------------------------------------------------------------- row writers

type paRowWriter interface {
	Write(rec map[string]any) error
	Flush() error // push buffered rows out (called per page so pipes see rows as they arrive)
	Close() error
}

func paNewRowWriter(format string, w io.Writer, columns []string) (paRowWriter, error) {
	switch format {
	case "json":
		return &paJSONWriter{w: w}, nil
	case "jsonl":
		return &paJSONLWriter{w: w}, nil
	case "csv":
		cw := csv.NewWriter(w)
		header := []string{}
		seen := map[string]bool{}
		for _, h := range append(append([]string{}, columns...), "timestamp") {
			if !seen[h] {
				seen[h] = true
				header = append(header, h)
			}
		}
		if err := cw.Write(header); err != nil {
			return nil, err
		}
		return &paCSVWriter{w: cw, header: header}, nil
	}
	return nil, fmt.Errorf("unknown format %q", format)
}

// paJSONWriter streams a valid JSON array (closed even after a mid-stream
// error so the file still parses; the non-zero exit code flags it incomplete).
type paJSONWriter struct {
	w     io.Writer
	began bool
}

func (j *paJSONWriter) Write(rec map[string]any) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	prefix := ",\n"
	if !j.began {
		prefix, j.began = "[\n", true
	}
	_, err = fmt.Fprintf(j.w, "%s%s", prefix, b)
	return err
}

func (j *paJSONWriter) Flush() error { return nil }

func (j *paJSONWriter) Close() error {
	var err error
	if !j.began {
		_, err = io.WriteString(j.w, "[]\n")
	} else {
		_, err = io.WriteString(j.w, "\n]\n")
	}
	return err
}

type paJSONLWriter struct{ w io.Writer }

func (j *paJSONLWriter) Write(rec map[string]any) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(j.w, "%s\n", b)
	return err
}
func (j *paJSONLWriter) Flush() error { return nil }
func (j *paJSONLWriter) Close() error { return nil }

type paCSVWriter struct {
	w      *csv.Writer
	header []string
}

func (c *paCSVWriter) Write(rec map[string]any) error {
	row := make([]string, len(c.header))
	for i, h := range c.header {
		row[i] = paCell(rec[h])
	}
	return c.w.Write(row)
}

func (c *paCSVWriter) Flush() error {
	c.w.Flush()
	return c.w.Error()
}

func (c *paCSVWriter) Close() error {
	c.w.Flush()
	return c.w.Error()
}

func paCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// paWriteRowsCSV writes tidy rows with the union of keys, first-seen order.
func paWriteRowsCSV(w io.Writer, rows []map[string]any) error {
	var header []string
	seen := map[string]bool{}
	for _, r := range rows {
		keys := make([]string, 0, len(r))
		for k := range r {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			seen[k] = true
			header = append(header, k)
		}
	}
	// time/time_ms first, value last, groups in between.
	header = paOrderHeader(header)
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		row := make([]string, len(header))
		for i, h := range header {
			row[i] = paCell(r[h])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func paOrderHeader(h []string) []string {
	var first, mid, last []string
	for _, k := range h {
		switch k {
		case "time", "time_ms":
			first = append(first, k)
		case "value":
			last = append(last, k)
		default:
			mid = append(mid, k)
		}
	}
	sort.Strings(first) // "time" < "time_ms"
	return append(append(first, mid...), last...)
}

// ---------------------------------------------------------------- response flattening

// paScalarRows flattens the columnar scalar response: group columns
// ({"type":"group","values":[["action"],...]}) plus number columns. The first
// number column is "value"; extra ones keep their column name.
func paScalarRows(raw json.RawMessage) ([]map[string]any, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Columns []struct {
					Name   string            `json:"name"`
					Type   string            `json:"type"`
					Values []json.RawMessage `json:"values"`
				} `json:"columns"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode pa scalar response: %w", err)
	}
	cols := resp.Data.Attributes.Columns
	n := 0
	for _, c := range cols {
		n = max(n, len(c.Values))
	}
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{}
	}
	seenNumber := false
	for _, c := range cols {
		name := c.Name
		if c.Type != "group" {
			if !seenNumber {
				name, seenNumber = "value", true
			}
		}
		for i, v := range c.Values {
			rows[i][name] = paUnwrapGroup(v, c.Type == "group")
		}
	}
	return rows, nil
}

// paUnwrapGroup decodes a column cell; single-element group arrays collapse to the element.
func paUnwrapGroup(v json.RawMessage, group bool) any {
	var x any
	d := json.NewDecoder(bytes.NewReader(v))
	d.UseNumber()
	if d.Decode(&x) != nil {
		return string(v)
	}
	if arr, ok := x.([]any); ok && group && len(arr) == 1 {
		return arr[0]
	}
	return x
}

// paTimeseriesRows flattens {times:[ms..], series:[{group_tags:["facet:val"]}], values:[[..],..]}.
func paTimeseriesRows(raw json.RawMessage) ([]map[string]any, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Times  []int64 `json:"times"`
				Series []struct {
					GroupTags []string `json:"group_tags"`
				} `json:"series"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode pa timeseries response: %w", err)
	}
	a := resp.Data.Attributes
	if len(a.Series) != len(a.Values) {
		return nil, fmt.Errorf("pa timeseries response: %d series but %d value arrays", len(a.Series), len(a.Values))
	}
	var rows []map[string]any
	for si, s := range a.Series {
		if len(a.Values[si]) != len(a.Times) {
			return nil, fmt.Errorf("pa timeseries response: series %d has %d values for %d times", si, len(a.Values[si]), len(a.Times))
		}
		for ti, t := range a.Times {
			row := map[string]any{
				"time":    time.UnixMilli(t).UTC().Format(time.RFC3339),
				"time_ms": t,
			}
			for _, tag := range s.GroupTags {
				k, v, ok := strings.Cut(tag, ":")
				if !ok {
					k, v = tag, ""
				}
				row[k] = v
			}
			row["value"] = paUnwrapGroup(a.Values[si][ti], false)
			rows = append(rows, row)
		}
	}
	return rows, nil
}
