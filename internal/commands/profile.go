package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/nicolasacchi/ddx/internal/client"
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

// profilerValidDiffBy restricts `diff --by` to the two views that support a
// two-sided join (endpoint identity, function+file identity). "summary"
// isn't offered here — a diff of totals-across-everything isn't a diff.
var profilerValidDiffBy = map[string]bool{
	"endpoint": true,
	"function": true,
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
	profileEventID       string
	profileProfileID     string
)

func init() {
	rootCmd.AddCommand(profileCmd)
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileAggregateCmd)
	profileCmd.AddCommand(profileSummaryCmd)
	profileCmd.AddCommand(profileDiffCmd)
	profileCmd.AddCommand(profileGetCmd)

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
	profileDiffCmd.Flags().StringVar(&profileBy, "by", defaultProfileBy,
		"Diff view: endpoint (per-endpoint delta) or function (per-function delta, function+file identity)")
	profileDiffCmd.Flags().IntVar(&profileTopN, "top", 20, "Top N rows by absolute delta")

	profileGetCmd.Flags().StringVar(&profileEventID, "event-id", "",
		"Profile event id, the long base64 string from `ddx profile list` field `id` (required)")
	profileGetCmd.Flags().StringVar(&profileProfileID, "profile-id", "",
		"Profile id, the short base64 string from `ddx profile list` field `profile-id` (required)")
	profileGetCmd.Flags().StringVar(&profileBy, "by", "info",
		"View: info (rich metadata + GC stats), endpoint (per-endpoint top), function (flame leaves), summary (totals)")
	profileGetCmd.Flags().StringVar(&profileType, "type", defaultProfileType,
		"Profile type for endpoint/function/summary views: cpu-time, wall-time, alloc-samples, heap-live-samples, heap-live-size")
	profileGetCmd.Flags().IntVar(&profileTopN, "top", 20, "Top N results for endpoint/function views")
	_ = profileGetCmd.MarkFlagRequired("event-id")
	_ = profileGetCmd.MarkFlagRequired("profile-id")
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
		if err := profilerCheckEndpointFilterTag(profileQuery); err != nil {
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
		profilerResolveByFlag(cmd, defaultProfileBy)
		if !validProfileTypes[profileType] {
			return invalidProfileTypeError(profileType)
		}
		if !validProfileBy[profileBy] {
			return fmt.Errorf("invalid --by %q, want one of: endpoint, function, summary", profileBy)
		}
		if err := profilerCheckEndpointFilterTag(profileQuery); err != nil {
			return err
		}
		if msg := profilerHeapSamplesEndpointWarning(profileType, profileBy); msg != "" {
			fmt.Fprintln(os.Stderr, "warning: "+msg)
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
		if err := profilerCheckEndpointFilterTag(profileQuery); err != nil {
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
	Short: "Delta between two arbitrary scopes (versions, pods, time windows, etc.) — by endpoint or function",
	Long: `Compare profiler data between two filter scopes.
Useful for "did this PR introduce a regression" or "is one canary pod
allocating more than another."

You must specify each side either by version tag (--before-version / --after-version)
which compose into "version:vXXX" clauses, OR by an arbitrary --before-query /
--after-query string for non-version comparisons (pods, time slices, etc.).

--by endpoint (default) diffs per-endpoint totals, joined by endpoint name.
--by function diffs per-function totals from the flame graph, joined by
(function, file) identity — frame indices aren't stable across two
independently-captured aggregate responses, so function/file is the only
safe join key.

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
    --after-query  "kube_deployment:web" --from 1h

  # Per-function delta instead of per-endpoint
  ddx profile diff --service web-1000farmacie --type alloc-samples --by function \
    --before-version v2026.4.57 --after-version v2026.4.58 --from 2d --top 30`,
	RunE: func(cmd *cobra.Command, args []string) error {
		profilerResolveByFlag(cmd, defaultProfileBy)
		if !validProfileTypes[profileType] {
			return invalidProfileTypeError(profileType)
		}
		if !profilerValidDiffBy[profileBy] {
			return fmt.Errorf("invalid --by %q for `diff`, want one of: endpoint, function", profileBy)
		}
		if err := profilerCheckEndpointFilterTag(profileQuery); err != nil {
			return err
		}
		if err := profilerCheckEndpointFilterTag(profileBeforeQuery); err != nil {
			return err
		}
		if err := profilerCheckEndpointFilterTag(profileAfterQuery); err != nil {
			return err
		}
		if msg := profilerHeapSamplesEndpointWarning(profileType, profileBy); msg != "" {
			fmt.Fprintln(os.Stderr, "warning: "+msg)
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

		beforeWindow, err := profilerExtractWindowMeta(beforeRaw)
		if err != nil {
			return fmt.Errorf("before parse: %w", err)
		}
		afterWindow, err := profilerExtractWindowMeta(afterRaw)
		if err != nil {
			return fmt.Errorf("after parse: %w", err)
		}
		if msg := profilerDiffRepresentativenessWarning(beforeWindow.ProfilesAggregated, beforeWindow.ProfilesInWindow, afterWindow.ProfilesAggregated, afterWindow.ProfilesInWindow); msg != "" {
			fmt.Fprintln(os.Stderr, "warning: "+msg)
		}

		var out map[string]any
		switch profileBy {
		case "function":
			beforeFns, err := profilerExtractFunctionTotals(beforeRaw)
			if err != nil {
				return fmt.Errorf("before parse: %w", err)
			}
			afterFns, err := profilerExtractFunctionTotals(afterRaw)
			if err != nil {
				return fmt.Errorf("after parse: %w", err)
			}
			out = profilerBuildFunctionDiff(beforeFns, afterFns, beforeLabel, afterLabel, profileType, profileTopN, beforeWindow.Metadata, afterWindow.Metadata)
		default: // "endpoint"
			beforeEndpoints, _, err := extractEndpointValues(beforeRaw)
			if err != nil {
				return fmt.Errorf("before parse: %w", err)
			}
			afterEndpoints, _, err := extractEndpointValues(afterRaw)
			if err != nil {
				return fmt.Errorf("after parse: %w", err)
			}
			out = buildEndpointDiff(beforeEndpoints, afterEndpoints, beforeLabel, afterLabel, profileType, profileTopN, beforeWindow.Metadata, afterWindow.Metadata)
		}

		jsonBytes, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return printData("", jsonBytes)
	},
}

// ----------------------------------------------------------------------------
// get — single-profile metadata + flame graph
//
// API:
//   GET  /profiling/api/v1/profiles/{profileId}/info?eventId=X&eventScope=profile
//   POST /profiling/api/v1/aggregate (with paired profileIds + eventIds + eventScopes)
//
// The list endpoint returns BOTH IDs per profile:
//   data[i].id              — long base64 (the eventId)
//   data[i].attributes.id   — short base64 (the profileId)
//
// In our flattened `ddx profile list` output these surface as `id` (eventId)
// and `profile-id` (profileId) — pipe both into `ddx profile get`.
// ----------------------------------------------------------------------------

var profileGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Single-profile drill-down — rich metadata + per-profile flame graph",
	Long: `Pull a single profile's data by its (event-id, profile-id) pair.

Default --by info returns the full per-profile metadata: profileStart/End, host,
all 60+ tags, system info (runtime version, kernel), GC stats (heap_marked_slots,
minor_gc_count, major_gc_count, total_allocated_objects), allocation sampling
stats, and full profiler settings. This is the closest thing we have to the
runtime.ruby.* metrics that aren't shipping to Datadog.

--by endpoint|function|summary returns the flame-graph data for THAT one profile
(equivalent to clicking on a profile in the UI explorer's stream view).

Examples:
  # Pick a profile from the list, then drill in for full metadata
  ddx profile list --service web-1000farmacie --query "kube_deployment:web-canary" \
    --from 1h --limit 5 --jq '0.{event:id,profile:"profile-id",pod:tag.pod_name}'
  # Then:
  ddx profile get --event-id "AwAAAZ33...AAAA" --profile-id "AZ33SqdJ...AA"

  # Per-profile flame leaves (what's hot in this specific 60s sample?)
  ddx profile get --event-id E --profile-id P --by function --top 20

  # Per-profile endpoint hotspots (which endpoints did this pod serve in this 60s?)
  ddx profile get --event-id E --profile-id P --by endpoint --top 10 --type alloc-samples`,
	RunE: func(cmd *cobra.Command, args []string) error {
		profilerResolveByFlag(cmd, "info")
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		switch profileBy {
		case "info":
			return runProfileGetInfo(c)
		case "endpoint":
			if !validProfileTypes[profileType] {
				return invalidProfileTypeError(profileType)
			}
			if msg := profilerHeapSamplesEndpointWarning(profileType, profileBy); msg != "" {
				fmt.Fprintln(os.Stderr, "warning: "+msg)
			}
			raw, err := callSingleProfileAggregate(c, profileType)
			if err != nil {
				return err
			}
			return printProfileEndpointView(raw, profileType, profileTopN)
		case "function":
			if !validProfileTypes[profileType] {
				return invalidProfileTypeError(profileType)
			}
			raw, err := callSingleProfileAggregate(c, profileType)
			if err != nil {
				return err
			}
			return printProfileFunctionView(raw, profileType, profileTopN)
		case "summary":
			raw, err := callSingleProfileAggregate(c, defaultProfileType)
			if err != nil {
				return err
			}
			return printProfileSummaryView(raw)
		default:
			return fmt.Errorf("invalid --by %q for `get`, want one of: info, endpoint, function, summary", profileBy)
		}
	},
}

// runProfileGetInfo calls the per-profile info endpoint and prints the body.
func runProfileGetInfo(c *client.Client) error {
	path := fmt.Sprintf("profiling/api/v1/profiles/%s/info", profileProfileID)
	params := url.Values{}
	params.Set("eventId", profileEventID)
	params.Set("eventScope", "profile")
	raw, err := c.Get(context.Background(), path, params)
	if err != nil {
		return err
	}
	return printData("", raw)
}

// callSingleProfileAggregate POSTs the per-profile aggregate body to the same
// /aggregate endpoint, but using paired profileIds+eventIds+eventScopes arrays
// (which select a single profile by ID instead of running a query).
//
// The from/to fields must be present (zero epoch is fine — API ignores time
// when profileIds are specified) per the schema. randomizeProfiles=false to
// preserve deterministic single-profile mapping.
func callSingleProfileAggregate(c *client.Client, profType string) (json.RawMessage, error) {
	body := map[string]any{
		"profileIds":          []string{profileProfileID},
		"eventIds":            []string{profileEventID},
		"eventScopes":         []string{"profile"},
		"profileType":         profType,
		"aggregationFunction": "sum",
		"attribute":           "line",
		"limit":               1,
		"from":                "1970-01-01T00:00:00.000Z",
		"to":                  "1970-01-01T00:00:00.000Z",
		"randomizeProfiles":   false,
	}
	return c.Post(context.Background(), profileAggregateEndpoint, body)
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

	// heap-live-samples gets a deterministic pre-flight warning instead (see
	// profilerHeapSamplesEndpointWarning) — Ruby's retained-heap sampler
	// NEVER attributes those samples to an endpoint, so there's no need to
	// wait for the response to know it's uninformative. heap-live-size isn't
	// guaranteed the same way, so it keeps this data-driven check: warn only
	// when _UNASSIGNED_ actually swamps the result.
	if profType == "heap-live-size" && total > 0 {
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
		"profile_type":        profType,
		"aggregation":         resp.AggregationFunction,
		"profiles_aggregated": resp.NumberOfProfiles,
		"profiles_in_window":  resp.TotalProfilesCount,
		"endpoints_total":     len(resp.EndpointValues),
		"total":               total,
		"top":                 endpoints,
		"metadata":            resp.Metadata,
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
	Endpoint   string  `json:"endpoint"`
	Before     float64 `json:"before"`
	After      float64 `json:"after"`
	Delta      float64 `json:"delta"`
	PercentChg float64 `json:"percent_change"`
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
		"profile_type":     profType,
		"before_version":   beforeVer,
		"after_version":    afterVer,
		"before_endpoints": len(before),
		"after_endpoints":  len(after),
		"top_by_abs_delta": rows,
		"before_metadata":  beforeMeta,
		"after_metadata":   afterMeta,
	}
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ----------------------------------------------------------------------------
// Pre-flight validations
// ----------------------------------------------------------------------------

// profilerResolveByFlag works around a pflag quirk: profileBy is a single
// package-level variable bound via Flags().StringVar on three commands
// (aggregate, get, diff) with DIFFERENT defaults ("endpoint", "info",
// "endpoint"). StringVar assigns its default into the bound variable
// IMMEDIATELY at registration time (init()), not at parse time — so
// whichever command's init() registration runs last leaves its default
// sitting in profileBy for every command that doesn't explicitly pass --by,
// regardless of which command is actually running. Call this at the top of
// each such RunE, before reading profileBy, to restore the running command's
// own default when the user didn't pass --by.
func profilerResolveByFlag(cmd *cobra.Command, ownDefault string) {
	if !cmd.Flags().Changed("by") {
		profileBy = ownDefault
	}
}

// profilerCheckEndpointFilterTag pre-flight-rejects a user-supplied Datadog
// query containing "@endpoint:". Profiles carry no such facet — the API
// accepts the query but silently matches nothing (profiles_in_window: 0),
// which otherwise looks like an empty time window rather than a bad filter.
func profilerCheckEndpointFilterTag(query string) error {
	if strings.Contains(query, "@endpoint:") {
		return fmt.Errorf("@endpoint: is not a filter tag on profiles (returns profiles_in_window: 0); filter by endpoint via --by endpoint output instead")
	}
	return nil
}

// profilerHeapSamplesEndpointWarning returns the pre-flight stderr warning
// for --type heap-live-samples combined with --by endpoint, or "" when the
// combination doesn't apply. Ruby's retained-heap sampler never attributes a
// sample to an endpoint (they all land in _UNASSIGNED_), so this is knowable
// from the flags alone — no need to wait for the API round-trip to find out.
func profilerHeapSamplesEndpointWarning(profType, by string) string {
	if profType == "heap-live-samples" && by == "endpoint" {
		return "--type heap-live-samples --by endpoint is uninformative — Ruby's retained-heap profiler doesn't tag samples with endpoints (all land in _UNASSIGNED_). Use --by function instead."
	}
	return ""
}

// profilerDiffRepresentativenessWarning returns a stderr-ready warning when a
// diff comparison looks unrepresentative — either side matched zero profiles
// in its window, or the two sides pulled in wildly different profile counts
// (more than 5x apart) — or "" when the comparison looks sound.
func profilerDiffRepresentativenessWarning(beforeAggregated, beforeInWindow, afterAggregated, afterInWindow int) string {
	if beforeInWindow == 0 || afterInWindow == 0 {
		return fmt.Sprintf("comparison may be unrepresentative — profiles_in_window is 0 on at least one side (before=%d, after=%d)", beforeInWindow, afterInWindow)
	}
	lo, hi := beforeAggregated, afterAggregated
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == 0 || float64(hi)/float64(lo) > 5.0 {
		return fmt.Sprintf("comparison may be unrepresentative — profiles_aggregated differ by more than 5x (before=%d, after=%d)", beforeAggregated, afterAggregated)
	}
	return ""
}

// ----------------------------------------------------------------------------
// Function-diff helpers (`diff --by function`)
// ----------------------------------------------------------------------------

// profilerWindowMeta carries the small set of window-level fields shared by
// both diff paths (endpoint and function): how many profiles were combined
// into this aggregate call, how many profiles matched the query in the time
// window before any limit/sampling, and the metadata blob to echo back.
type profilerWindowMeta struct {
	ProfilesAggregated int
	ProfilesInWindow   int
	Metadata           json.RawMessage
}

// profilerExtractWindowMeta pulls the window-level counters out of a raw
// aggregate response, independent of which --by view is being rendered.
func profilerExtractWindowMeta(raw json.RawMessage) (profilerWindowMeta, error) {
	var resp aggregateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return profilerWindowMeta{}, err
	}
	return profilerWindowMeta{
		ProfilesAggregated: resp.NumberOfProfiles,
		ProfilesInWindow:   resp.TotalProfilesCount,
		Metadata:           resp.Metadata,
	}, nil
}

// profilerFunctionEntry is one (function, file) identity with its aggregated
// leaf value, as produced by profilerExtractFunctionTotals.
type profilerFunctionEntry struct {
	Function string
	File     string
	Value    float64
}

// profilerFunctionIdentityKey builds the join key used to match a function
// across two independently-captured aggregate responses. Frame indices are
// NOT stable across separate API calls — each response builds its own
// strings table from scratch — so joins must use the resolved (function,
// file) identity instead of the frame index that `aggregate --by function`
// groups by internally (safe there because it never leaves a single response).
func profilerFunctionIdentityKey(function, file string) string {
	return function + "\x00" + file
}

// profilerExtractFunctionTotals decodes a raw aggregate response's flame
// graph and aggregates leaf values by (function, file) identity. It reuses
// the same decode primitives (parseFlameNode / collectLeaves / resolveFrames)
// that `aggregate --by function` uses in profile_decode.go, rather than
// re-parsing the packed flame graph a second time — the only new step here
// is merging leaves that share a (function, file) identity but landed at
// different frame indices (recursion, multiple call sites, etc.), which
// aggregate --by function doesn't need to do since it never joins across
// two separate responses.
func profilerExtractFunctionTotals(raw json.RawMessage) ([]profilerFunctionEntry, error) {
	var resp struct {
		FlameGraph  json.RawMessage `json:"flameGraph"`
		Frames      [][]int         `json:"frames"`
		Strings     []string        `json:"strings"`
		FrameSchema []string        `json:"frameSchema"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse aggregate response: %w", err)
	}
	if len(resp.FlameGraph) == 0 {
		return nil, fmt.Errorf("response has no flameGraph; check --service / --query / --from / --type values")
	}

	root, err := parseFlameNode(resp.FlameGraph)
	if err != nil {
		return nil, fmt.Errorf("decode flame graph: %w", err)
	}

	leafByFrame := make(map[int]float64)
	collectLeaves(root, leafByFrame)

	frameInfos := resolveFrames(resp.Frames, resp.FrameSchema, resp.Strings)

	byKey := make(map[string]*profilerFunctionEntry)
	keyOrder := make([]string, 0, len(leafByFrame))
	for fIdx, value := range leafByFrame {
		var info frameInfo
		if fIdx >= 0 && fIdx < len(frameInfos) {
			info = frameInfos[fIdx]
		}
		key := profilerFunctionIdentityKey(info.Function, info.File)
		if e, ok := byKey[key]; ok {
			e.Value += value
		} else {
			byKey[key] = &profilerFunctionEntry{Function: info.Function, File: info.File, Value: value}
			keyOrder = append(keyOrder, key)
		}
	}

	entries := make([]profilerFunctionEntry, 0, len(byKey))
	for _, key := range keyOrder {
		entries = append(entries, *byKey[key])
	}
	return entries, nil
}

// profilerFunctionDiffRow is one row in the function-diff table — the
// function-view counterpart of diffRow.
type profilerFunctionDiffRow struct {
	Function   string  `json:"function"`
	File       string  `json:"file,omitempty"`
	Before     float64 `json:"before"`
	After      float64 `json:"after"`
	Delta      float64 `json:"delta"`
	PercentChg float64 `json:"percent_change"`
}

// profilerBuildFunctionDiff joins two function-total slices by (function,
// file) identity and computes per-function deltas + percent changes,
// mirroring buildEndpointDiff's shape and semantics. Functions present on
// only one side are still included (missing side = 0).
func profilerBuildFunctionDiff(before, after []profilerFunctionEntry, beforeVer, afterVer, profType string, topN int, beforeMeta, afterMeta json.RawMessage) map[string]any {
	beforeByKey := make(map[string]profilerFunctionEntry, len(before))
	for _, e := range before {
		beforeByKey[profilerFunctionIdentityKey(e.Function, e.File)] = e
	}
	afterByKey := make(map[string]profilerFunctionEntry, len(after))
	for _, e := range after {
		afterByKey[profilerFunctionIdentityKey(e.Function, e.File)] = e
	}

	seen := make(map[string]bool, len(beforeByKey)+len(afterByKey))
	for k := range beforeByKey {
		seen[k] = true
	}
	for k := range afterByKey {
		seen[k] = true
	}

	rows := make([]profilerFunctionDiffRow, 0, len(seen))
	for key := range seen {
		b := beforeByKey[key]
		a := afterByKey[key]
		function, file := b.Function, b.File
		if function == "" && a.Function != "" {
			function, file = a.Function, a.File
		}
		row := profilerFunctionDiffRow{
			Function: function,
			File:     file,
			Before:   b.Value,
			After:    a.Value,
			Delta:    a.Value - b.Value,
		}
		if b.Value > 0 {
			row.PercentChg = (a.Value - b.Value) / b.Value * 100.0
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
		"profile_type":     profType,
		"before_version":   beforeVer,
		"after_version":    afterVer,
		"before_functions": len(before),
		"after_functions":  len(after),
		"top_by_abs_delta": rows,
		"before_metadata":  beforeMeta,
		"after_metadata":   afterMeta,
	}
}

// (extractData is defined in logs.go and shared across the commands package.)
