package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// ============================================================================
// Datadog Continuous Profiler (`ddx profile`)
//
// Hits the same endpoints the Datadog UI uses to render the flame graph.
// Both endpoints accept standard DD-API-KEY + DD-APPLICATION-KEY auth.
//
//   POST /profiling/api/v1/aggregate    — flame graph + per-endpoint hotspots
//   POST /api/unstable/profiles/list    — individual profile metadata
//
// Subcommands:
//   ddx profile list      list individual profiles (metadata only)
//   ddx profile aggregate flame-graph aggregation (--by endpoint|function|summary)
//   ddx profile summary   shorthand for `aggregate --by summary --limit 1`
//   ddx profile diff      per-endpoint delta between two image versions
// ============================================================================

const (
	profileAggregateEndpoint = "profiling/api/v1/aggregate"
	profileListEndpoint      = "api/unstable/profiles/list"

	defaultProfileType = "cpu-time"
	defaultProfileBy   = "endpoint"
)

// validProfileTypes are the profileType values currently accepted by the
// /profiling/api/v1/aggregate endpoint for Ruby. `alloc-bytes` is NOT supported
// (Ruby profiler emits allocation count, not byte size — HTTP 400).
var validProfileTypes = map[string]bool{
	"cpu-time":          true,
	"wall-time":         true,
	"alloc-samples":     true,
	"heap-live-samples": true,
	"heap-live-size":    true,
}

var validProfileBy = map[string]bool{
	"endpoint": true,
	"function": true,
	"summary":  true,
}

// Subcommand-scoped flag vars. Cobra binds the same variable on multiple
// commands; only the currently-running command's flag populates it.
var (
	profileService       string
	profileEnv           string
	profileQuery         string
	profileType          string
	profileBy            string
	profileTopN          int
	profileBeforeVersion string
	profileAfterVersion  string
	profileBeforeQuery   string
	profileAfterQuery    string
)

func init() {
	rootCmd.AddCommand(profileCmd)
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileAggregateCmd)
	profileCmd.AddCommand(profileSummaryCmd)
	profileCmd.AddCommand(profileDiffCmd)

	// Shared flags helper. Cobra holds one variable per Var() call but only the
	// running command's flag mutates it — safe pattern, used elsewhere in ddx.
	addShared := func(c *cobra.Command) {
		c.Flags().StringVar(&profileService, "service", "", "Service name (required)")
		c.Flags().StringVar(&profileEnv, "env", "production", "Environment")
		c.Flags().StringVar(&profileQuery, "query", "", "Additional Datadog filter (e.g. 'kube_deployment:web-canary')")
		_ = c.MarkFlagRequired("service")
	}
	addShared(profileListCmd)
	addShared(profileAggregateCmd)
	addShared(profileSummaryCmd)
	addShared(profileDiffCmd)

	profileAggregateCmd.Flags().StringVar(&profileType, "type", defaultProfileType,
		"Profile type: cpu-time, wall-time, alloc-samples, heap-live-samples, heap-live-size")
	profileAggregateCmd.Flags().StringVar(&profileBy, "by", defaultProfileBy,
		"Aggregate view: endpoint (per-endpoint top), function (flame leaves), summary (totals)")
	profileAggregateCmd.Flags().IntVar(&profileTopN, "top", 20, "Top N results to display")

	profileDiffCmd.Flags().StringVar(&profileType, "type", defaultProfileType, "Profile type (see aggregate --type)")
	profileDiffCmd.Flags().StringVar(&profileBeforeVersion, "before-version", "",
		"Image version tag for the 'before' side, e.g. v2026.4.57 (one of --before-version / --before-query required)")
	profileDiffCmd.Flags().StringVar(&profileAfterVersion, "after-version", "",
		"Image version tag for the 'after' side, e.g. v2026.4.58 (one of --after-version / --after-query required)")
	profileDiffCmd.Flags().StringVar(&profileBeforeQuery, "before-query", "",
		"Arbitrary Datadog filter for the 'before' side (alternative to --before-version), e.g. 'pod_name:web-canary-X' or '@timestamp:[now-2h TO now-1h]'")
	profileDiffCmd.Flags().StringVar(&profileAfterQuery, "after-query", "",
		"Arbitrary Datadog filter for the 'after' side (alternative to --after-version)")
	profileDiffCmd.Flags().IntVar(&profileTopN, "top", 20, "Top N endpoints by absolute delta")
}

// ----------------------------------------------------------------------------
// Parent command
// ----------------------------------------------------------------------------

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Datadog Continuous Profiler — list, aggregate, summary, diff",
	Long: `Query Datadog Continuous Profiler data via the same endpoints the UI uses.

API: POST /profiling/api/v1/aggregate (aggregate, summary, diff)
     POST /api/unstable/profiles/list  (list)

Returns flame graph + per-endpoint hotspots in JSON form. The UI's flame graph
is rendered client-side from exactly this data.

Examples:
  ddx profile list      --service web-1000farmacie --query "kube_deployment:web-canary" --from 1h
  ddx profile aggregate --service web-1000farmacie --type alloc-samples --by endpoint --top 10 --from 7d
  ddx profile aggregate --service web-1000farmacie --type cpu-time --by function --top 20 --from 1h
  ddx profile summary   --service web-1000farmacie --from 1h
  ddx profile diff      --service web-1000farmacie --type alloc-samples \
                        --before-version v2026.4.57 --after-version v2026.4.58 --from 2d`,
}

// ----------------------------------------------------------------------------
// list — POST /api/unstable/profiles/list
// ----------------------------------------------------------------------------

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List individual profile metadata (id, pod, version, size, duration)",
	Long: `List profiles matching a query. Response includes per-profile metadata:
profile id, host, pod_name, version, profiler_version, duration, ingest_size_in_bytes,
plus full tag set. Use --limit to control how many profiles are returned.

Examples:
  ddx profile list --service web-1000farmacie --from 1h
  ddx profile list --service web-1000farmacie --query "kube_deployment:web-canary" --from 7d --limit 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		from, to, err := parseProfileTimeRange()
		if err != nil {
			return err
		}

		body := map[string]any{
			"filter": map[string]any{
				"query": buildProfileQuery(""),
				"from":  timeToISO(from),
				"to":    timeToISO(to),
			},
			"page": map[string]any{"limit": limitFlag},
		}

		raw, err := c.Post(context.Background(), profileListEndpoint, body)
		if err != nil {
			return err
		}

		// Response shape: {"data":[{"type":"profile","attributes":{...}}]}
		// Flatten to per-attribute objects (id merged in).
		extracted := extractData(raw)
		flat := flattenV2Items(extracted)
		return printData("", flat)
	},
}

// ----------------------------------------------------------------------------
// aggregate — POST /profiling/api/v1/aggregate
// ----------------------------------------------------------------------------

var profileAggregateCmd = &cobra.Command{
	Use:   "aggregate",
	Short: "Aggregate flame graph data — per-endpoint, per-function, or summary totals",
	Long: `Aggregate continuous profiler samples over a time range.

--by endpoint  → top-N endpoints by chosen profile type, with % of total
--by function  → top-N hot leaves (function:file:line) from the flame graph
--by summary   → totals across all profile types (cpu, alloc, heap, wall) + window metadata

The endpoint view is the headline answer to "which endpoints allocated the most"
or "which endpoints used the most CPU." It's the single most actionable view.

Examples:
  # Top 20 endpoints by allocation samples on web-canary, last 7 days
  ddx profile aggregate --service web-1000farmacie \
    --query "kube_deployment:web-canary" --type alloc-samples --by endpoint --top 20 --from 7d

  # Top 30 hot functions by CPU time
  ddx profile aggregate --service web-1000farmacie --type cpu-time --by function --top 30 --from 1h

  # Quick totals
  ddx profile aggregate --service web-1000farmacie --by summary --from 1h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !validProfileTypes[profileType] {
			return invalidProfileTypeError(profileType)
		}
		if !validProfileBy[profileBy] {
			return fmt.Errorf("invalid --by %q, want one of: endpoint, function, summary", profileBy)
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		from, to, err := parseProfileTimeRange()
		if err != nil {
			return err
		}

		raw, err := callProfileAggregate(c, buildProfileQuery(""), from, to, profileType, limitFlag)
		if err != nil {
			return err
		}

		switch profileBy {
		case "endpoint":
			return printProfileEndpointView(raw, profileType, profileTopN)
		case "function":
			return printProfileFunctionView(raw, profileType, profileTopN)
		case "summary":
			return printProfileSummaryView(raw)
		}
		return nil
	},
}

// ----------------------------------------------------------------------------
// summary — alias for `aggregate --by summary --limit 1`
// ----------------------------------------------------------------------------

var profileSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Quick profile-window summary (totals across cpu/alloc/heap/wall)",
	Long: `Shorthand for 'aggregate --by summary --limit 1'.
Returns: window metadata (service, host, profileStart/End), totals across all profile
types in summaryValues + summaryDurations, profile counts, and emitted profile IDs.

Examples:
  ddx profile summary --service web-1000farmacie --from 1h
  ddx profile summary --service web-1000farmacie --query "kube_deployment:web-canary" --from 24h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		from, to, err := parseProfileTimeRange()
		if err != nil {
			return err
		}

		// Use the inherited --limit (default 50) so the API has enough profiles
		// to produce non-empty summaryValues. limit=1 returns empty totals.
		raw, err := callProfileAggregate(c, buildProfileQuery(""), from, to, defaultProfileType, limitFlag)
		if err != nil {
			return err
		}
		return printProfileSummaryView(raw)
	},
}

// ----------------------------------------------------------------------------
// diff — two aggregate calls scoped by version, per-endpoint delta
// ----------------------------------------------------------------------------

var profileDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Per-endpoint delta between two arbitrary scopes (versions, pods, time windows, etc.)",
	Long: `Compare profiler endpointValues between two filter scopes.
Useful for "did this PR introduce a regression" or "is one canary pod
allocating more than another."

You must specify each side either by version tag (--before-version / --after-version)
which compose into "version:vXXX" clauses, OR by an arbitrary --before-query /
--after-query string for non-version comparisons (pods, time slices, etc.).

Examples:
  # Did v2026.4.58 increase allocation rate vs v2026.4.57?
  ddx profile diff --service web-1000farmacie --type alloc-samples \
    --before-version v2026.4.57 --after-version v2026.4.58 --from 2d

  # CPU regression check on canary
  ddx profile diff --service web-1000farmacie --type cpu-time \
    --query "kube_deployment:web-canary" \
    --before-version v2026.4.42 --after-version v2026.4.43 --from 5d --top 30

  # Compare two specific pods
  ddx profile diff --service web-1000farmacie --type alloc-samples \
    --before-query "pod_name:web-canary-abc-1" \
    --after-query  "pod_name:web-canary-abc-2" --from 1h

  # Canary vs primary deployment (different deployments)
  ddx profile diff --service web-1000farmacie --type alloc-samples \
    --before-query "kube_deployment:web-canary" \
    --after-query  "kube_deployment:web" --from 1h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !validProfileTypes[profileType] {
			return invalidProfileTypeError(profileType)
		}
		// Resolve before/after filter clauses; require at least one form per side.
		beforeClause, beforeLabel, err := resolveDiffSide("before", profileBeforeVersion, profileBeforeQuery)
		if err != nil {
			return err
		}
		afterClause, afterLabel, err := resolveDiffSide("after", profileAfterVersion, profileAfterQuery)
		if err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		from, to, err := parseProfileTimeRange()
		if err != nil {
			return err
		}

		beforeQuery := buildProfileQuery(beforeClause)
		afterQuery := buildProfileQuery(afterClause)

		beforeRaw, err := callProfileAggregate(c, beforeQuery, from, to, profileType, limitFlag)
		if err != nil {
			return fmt.Errorf("before query failed: %w", err)
		}
		afterRaw, err := callProfileAggregate(c, afterQuery, from, to, profileType, limitFlag)
		if err != nil {
			return fmt.Errorf("after query failed: %w", err)
		}

		beforeEndpoints, beforeMeta, err := extractEndpointValues(beforeRaw)
		if err != nil {
			return fmt.Errorf("before parse: %w", err)
		}
		afterEndpoints, afterMeta, err := extractEndpointValues(afterRaw)
		if err != nil {
			return fmt.Errorf("after parse: %w", err)
		}

		out := buildEndpointDiff(beforeEndpoints, afterEndpoints, beforeLabel, afterLabel, profileType, profileTopN, beforeMeta, afterMeta)
		jsonBytes, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return printData("", jsonBytes)
	},
}

// resolveDiffSide picks the clause + label for one diff side ("before" or
// "after"). Exactly one of version or query must be set per side.
func resolveDiffSide(side, version, query string) (clause, label string, err error) {
	hasVersion := version != ""
	hasQuery := query != ""
	if hasVersion && hasQuery {
		return "", "", fmt.Errorf("--%s-version and --%s-query are mutually exclusive (set one)", side, side)
	}
	if !hasVersion && !hasQuery {
		return "", "", fmt.Errorf("must set --%s-version or --%s-query", side, side)
	}
	if hasVersion {
		return "version:" + version, version, nil
	}
	return query, query, nil
}

// invalidProfileTypeError emits a clear error for unsupported --type values,
// with a Ruby-specific hint when the user requested alloc-bytes.
func invalidProfileTypeError(profType string) error {
	const ruby = "(Ruby valid types: cpu-time, wall-time, alloc-samples, heap-live-samples, heap-live-size)"
	if profType == "alloc-bytes" {
		return fmt.Errorf("--type alloc-bytes is not supported by the Ruby profiler — it emits allocation count, not byte size. Use --type alloc-samples instead %s", ruby)
	}
	return fmt.Errorf("invalid --type %q %s", profType, ruby)
}

// ============================================================================
// Helpers
// ============================================================================

// parseProfileTimeRange wraps parseFrom/parseTo for consistency.
func parseProfileTimeRange() (int64, int64, error) {
	from, err := parseFrom()
	if err != nil {
		return 0, 0, err
	}
	to, err := parseTo()
	if err != nil {
		return 0, 0, err
	}
	return from, to, nil
}

// buildProfileQuery composes the Datadog query string from --service, --env,
// --query (user extra filter), and an optional extra clause (used by diff).
func buildProfileQuery(extra string) string {
	parts := []string{
		"service:" + profileService,
		"env:" + profileEnv,
	}
	if profileQuery != "" {
		parts = append(parts, profileQuery)
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}

// callProfileAggregate POSTs to the aggregate endpoint and returns the raw response.
func callProfileAggregate(c clientPoster, query string, from, to int64, profType string, limit int) (json.RawMessage, error) {
	if limit < 1 {
		limit = 100
	}
	body := map[string]any{
		"from":                timeToISO(from),
		"to":                  timeToISO(to),
		"query":               query,
		"limit":               limit,
		"profileType":         profType,
		"aggregationFunction": "sum",
		"attribute":           "line",
	}
	return c.Post(context.Background(), profileAggregateEndpoint, body)
}

// clientPoster is the subset of *client.Client we need — extracted so tests
// could stub it in the future.
type clientPoster interface {
	Post(ctx context.Context, path string, body any) (json.RawMessage, error)
}

// ----------------------------------------------------------------------------
// View renderers (per --by mode)
// ----------------------------------------------------------------------------

// aggregateResponse is the subset of fields we consume from the API response.
type aggregateResponse struct {
	Metadata            json.RawMessage    `json:"metadata"`
	ProfileType         string             `json:"profileType"`
	AggregationFunction string             `json:"aggregationFunction"`
	NumberOfProfiles    int                `json:"numberOfProfiles"`
	TotalProfilesCount  int                `json:"totalProfilesCount"`
	ProfileIds          []string           `json:"profileIds"`
	FlameGraph          json.RawMessage    `json:"flameGraph"`
	NodeSchema          []string           `json:"nodeSchema"`
	FrameSchema         []string           `json:"frameSchema"`
	FrameSchemas        []frameSchemaEntry `json:"frameSchemas"`
	Frames              [][]int            `json:"frames"`
	Strings             []string           `json:"strings"`
	SummaryValues       map[string]float64 `json:"summaryValues"`
	SummaryDurations    map[string]float64 `json:"summaryDurations"`
	EndpointValues      map[string]float64 `json:"endpointValues"`
	EndpointCounts      map[string]float64 `json:"endpointCounts"`
}

type frameSchemaEntry struct {
	Family string   `json:"family"`
	Fields []string `json:"fields"`
}

// endpointEntry is a row in the endpoint top-N output.
type endpointEntry struct {
	Endpoint string  `json:"endpoint"`
	Value    float64 `json:"value"`
	Percent  float64 `json:"percent_of_total"`
}

// printProfileEndpointView extracts endpointValues, sorts by value desc,
// computes percent of total (using summaryValues[profileType]), and emits
// a JSON view: {top: [...], total, profile_type, profiles_aggregated, profiles_in_window, metadata}.
//
// For heap-live-samples / heap-live-size profile types where _UNASSIGNED_
// dominates the result (>80 %), emits a stderr hint suggesting --by function
// because Ruby's heap profiler doesn't attribute retained memory to endpoints.
func printProfileEndpointView(raw json.RawMessage, profType string, topN int) error {
	var resp aggregateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse aggregate response: %w", err)
	}

	endpoints := make([]endpointEntry, 0, len(resp.EndpointValues))
	for name, v := range resp.EndpointValues {
		endpoints = append(endpoints, endpointEntry{Endpoint: name, Value: v})
	}
	sort.SliceStable(endpoints, func(i, j int) bool {
		return endpoints[i].Value > endpoints[j].Value
	})

	total := resp.SummaryValues[profType]
	if total == 0 {
		// Fall back to summing the endpoint values themselves.
		for _, e := range endpoints {
			total += e.Value
		}
	}
	for i := range endpoints {
		if total > 0 {
			endpoints[i].Percent = endpoints[i].Value / total * 100.0
		}
	}

	// For heap-live-* types, warn when _UNASSIGNED_ swamps the result —
	// Ruby's retained-heap samples lack endpoint attribution, so endpoint
	// view is uninformative; function view is what the user wants.
	if (profType == "heap-live-samples" || profType == "heap-live-size") && total > 0 {
		if v, ok := resp.EndpointValues["_UNASSIGNED_"]; ok && v/total > 0.80 {
			fmt.Fprintf(os.Stderr,
				"hint: --type %s --by endpoint is uninformative because Ruby's retained-heap profiler doesn't tag samples with endpoints (_UNASSIGNED_=%.0f %% here). Try --by function instead.\n",
				profType, v/total*100,
			)
		}
	}

	if topN > 0 && len(endpoints) > topN {
		endpoints = endpoints[:topN]
	}

	out := map[string]any{
		"profile_type":         profType,
		"aggregation":          resp.AggregationFunction,
		"profiles_aggregated":  resp.NumberOfProfiles,
		"profiles_in_window":   resp.TotalProfilesCount,
		"endpoints_total":      len(resp.EndpointValues),
		"total":                total,
		"top":                  endpoints,
		"metadata":             resp.Metadata,
	}
	jsonBytes, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return printData("", jsonBytes)
}

// printProfileSummaryView returns just the totals + window metadata (no flame graph).
func printProfileSummaryView(raw json.RawMessage) error {
	var resp aggregateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse aggregate response: %w", err)
	}
	out := map[string]any{
		"profiles_aggregated": resp.NumberOfProfiles,
		"profiles_in_window":  resp.TotalProfilesCount,
		"summary_values":      resp.SummaryValues,
		"summary_durations":   resp.SummaryDurations,
		"profile_ids":         resp.ProfileIds,
		"metadata":            resp.Metadata,
	}
	jsonBytes, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return printData("", jsonBytes)
}

// printProfileFunctionView decodes the packed flame graph and emits top-N
// hot leaves (function:file:line). Defined in profile_decode.go.
// The function is in this file as a stub so the package compiles even if
// profile_decode.go is missing; profile_decode.go provides the real impl.
//
// Real implementation lives in profile_decode.go; this file would have a
// declaration if needed. (Removed dummy: profile_decode.go is required.)

// ----------------------------------------------------------------------------
// Diff helpers
// ----------------------------------------------------------------------------

// extractEndpointValues parses a raw aggregate response and returns the
// endpointValues map plus the metadata for context.
func extractEndpointValues(raw json.RawMessage) (map[string]float64, json.RawMessage, error) {
	var resp aggregateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, nil, err
	}
	return resp.EndpointValues, resp.Metadata, nil
}

// diffRow is one row in the diff table.
type diffRow struct {
	Endpoint    string  `json:"endpoint"`
	Before      float64 `json:"before"`
	After       float64 `json:"after"`
	Delta       float64 `json:"delta"`
	PercentChg  float64 `json:"percent_change"`
}

// buildEndpointDiff joins two endpoint maps by endpoint name and computes
// per-endpoint deltas + percent changes. Endpoints present on only one side
// are still included (other side = 0).
func buildEndpointDiff(before, after map[string]float64, beforeVer, afterVer, profType string, topN int, beforeMeta, afterMeta json.RawMessage) map[string]any {
	// Union of endpoint names
	seen := make(map[string]bool, len(before)+len(after))
	for k := range before {
		seen[k] = true
	}
	for k := range after {
		seen[k] = true
	}

	rows := make([]diffRow, 0, len(seen))
	for name := range seen {
		b := before[name]
		a := after[name]
		row := diffRow{
			Endpoint: name,
			Before:   b,
			After:    a,
			Delta:    a - b,
		}
		if b > 0 {
			row.PercentChg = (a - b) / b * 100.0
		} else if a > 0 {
			row.PercentChg = 0 // can't compute when before is zero; leave as 0 with explicit Before=0 flagged
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return absF(rows[i].Delta) > absF(rows[j].Delta)
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}

	return map[string]any{
		"profile_type":      profType,
		"before_version":    beforeVer,
		"after_version":     afterVer,
		"before_endpoints":  len(before),
		"after_endpoints":   len(after),
		"top_by_abs_delta":  rows,
		"before_metadata":   beforeMeta,
		"after_metadata":    afterMeta,
	}
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// (extractData is defined in logs.go and shared across the commands package.)
