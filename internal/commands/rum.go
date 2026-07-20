package commands

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	rumQuery    string
	rumDetailed bool
)

// rumlogs* — package-prefixed identifiers owned by this file (rum.go) plus
// logs_config.go, kept distinct from every other commands-package file to
// avoid collisions on merge.
var (
	// rum aggregate
	rumlogsAggQuery   string
	rumlogsAggCompute string
	rumlogsAggGroupBy string

	// rum retention-filters (scoped by --app on the parent command)
	rumlogsRFApp        string
	rumlogsRFName       string
	rumlogsRFEventType  string
	rumlogsRFQuery      string
	rumlogsRFSampleRate float64
	rumlogsRFEnabled    bool

	// rum metrics
	rumlogsRMID                 string
	rumlogsRMEventType          string
	rumlogsRMCompute            string
	rumlogsRMIncludePercentiles bool
	rumlogsRMQuery              string
	rumlogsRMGroupBy            []string
	rumlogsRMUniquenessWhen     string
)

func init() {
	rootCmd.AddCommand(rumCmd)
	rumCmd.AddCommand(rumAppsCmd)
	rumCmd.AddCommand(rumEventsCmd)
	rumCmd.AddCommand(rumSessionsCmd)
	rumCmd.AddCommand(rumAggregateCmd)
	rumCmd.AddCommand(rumRetentionFiltersCmd)
	rumCmd.AddCommand(rumMetricsCmd)

	rumEventsCmd.Flags().StringVar(&rumQuery, "query", "@type:error", "RUM event query")
	rumEventsCmd.Flags().BoolVar(&rumDetailed, "detailed", false, "Return full event data")

	rumSessionsCmd.Flags().StringVar(&rumQuery, "query", "@type:session", "RUM session query")

	rumAggregateCmd.Flags().StringVar(&rumlogsAggQuery, "query", "*", "RUM search query")
	rumAggregateCmd.Flags().StringVar(&rumlogsAggCompute, "compute", "count", "Aggregation: count, avg(@field), sum(@field), min(@field), max(@field), cardinality(@field)")
	rumAggregateCmd.Flags().StringVar(&rumlogsAggGroupBy, "group-by", "", "Facet to group by (e.g., @view.url, @session.type)")

	// retention-filters: --app is required and shared by every subcommand.
	rumRetentionFiltersCmd.PersistentFlags().StringVar(&rumlogsRFApp, "app", "", "RUM application ID (required)")
	rumRetentionFiltersCmd.MarkPersistentFlagRequired("app")
	rumRetentionFiltersCmd.AddCommand(rumRFListCmd)
	rumRetentionFiltersCmd.AddCommand(rumRFGetCmd)
	rumRetentionFiltersCmd.AddCommand(rumRFCreateCmd)
	rumRetentionFiltersCmd.AddCommand(rumRFUpdateCmd)
	rumRetentionFiltersCmd.AddCommand(rumRFDeleteCmd)

	rumRFCreateCmd.Flags().StringVar(&rumlogsRFName, "name", "", "Retention filter name (required)")
	rumRFCreateCmd.Flags().StringVar(&rumlogsRFEventType, "event-type", "", "RUM event type: session, view, action, error, resource, long_task, vital (required)")
	rumRFCreateCmd.Flags().StringVar(&rumlogsRFQuery, "query", "", "RUM search query to filter on")
	rumRFCreateCmd.Flags().Float64Var(&rumlogsRFSampleRate, "sample-rate", 0, "Sample rate, between 0.1 and 100 (required)")
	rumRFCreateCmd.Flags().BoolVar(&rumlogsRFEnabled, "enabled", true, "Whether the retention filter is enabled")
	rumRFCreateCmd.MarkFlagRequired("name")
	rumRFCreateCmd.MarkFlagRequired("event-type")
	rumRFCreateCmd.MarkFlagRequired("sample-rate")

	rumRFUpdateCmd.Flags().StringVar(&rumlogsRFName, "name", "", "Retention filter name")
	rumRFUpdateCmd.Flags().StringVar(&rumlogsRFEventType, "event-type", "", "RUM event type: session, view, action, error, resource, long_task, vital")
	rumRFUpdateCmd.Flags().StringVar(&rumlogsRFQuery, "query", "", "RUM search query to filter on")
	rumRFUpdateCmd.Flags().Float64Var(&rumlogsRFSampleRate, "sample-rate", 0, "Sample rate, between 0.1 and 100")
	rumRFUpdateCmd.Flags().BoolVar(&rumlogsRFEnabled, "enabled", true, "Whether the retention filter is enabled")

	rumMetricsCmd.AddCommand(rumMetricsListCmd)
	rumMetricsCmd.AddCommand(rumMetricsGetCmd)
	rumMetricsCmd.AddCommand(rumMetricsCreateCmd)
	rumMetricsCmd.AddCommand(rumMetricsDeleteCmd)

	rumMetricsCreateCmd.Flags().StringVar(&rumlogsRMID, "id", "", "RUM-based metric name, e.g. rum.sessions.web.count (required)")
	rumMetricsCreateCmd.Flags().StringVar(&rumlogsRMEventType, "event-type", "", "RUM event type: session, view, action, error, resource, long_task, vital (required)")
	rumMetricsCreateCmd.Flags().StringVar(&rumlogsRMCompute, "compute", "", `Compute spec: "count" or "<aggregation_type>/<path>", e.g. "distribution/@duration" (required)`)
	rumMetricsCreateCmd.Flags().BoolVar(&rumlogsRMIncludePercentiles, "include-percentiles", false, "Include percentile aggregations (distribution metrics only)")
	rumMetricsCreateCmd.Flags().StringVar(&rumlogsRMQuery, "query", "", "RUM search query filter")
	rumMetricsCreateCmd.Flags().StringSliceVar(&rumlogsRMGroupBy, "group-by", nil, `Group-by rule(s) "path[:tag_name]", repeatable`)
	rumMetricsCreateCmd.Flags().StringVar(&rumlogsRMUniquenessWhen, "uniqueness-when", "", "When to count updatable events: match or end (session/view event types only)")
	rumMetricsCreateCmd.MarkFlagRequired("id")
	rumMetricsCreateCmd.MarkFlagRequired("event-type")
	rumMetricsCreateCmd.MarkFlagRequired("compute")
}

var rumCmd = &cobra.Command{
	Use:   "rum",
	Short: "Real User Monitoring — apps, events, sessions, aggregate, retention filters, metrics",
}

var rumAppsCmd = &cobra.Command{
	Use:   "apps",
	Short: "List RUM applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/rum/applications", nil)
		if err != nil {
			return err
		}

		// Extract data array and flatten
		items := extractData(data)
		return printData("rum.apps", flattenV2Items(items))
	},
}

var rumEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Search RUM events (all types: view, action, error, resource, etc.)",
	Long: `Search RUM events across all event types.

Event types: session, view, action, error, resource, long_task, vital

Examples:
  ddx rum events --query "@type:error" --from 1h
  ddx rum events --query "@type:view @view.loading_time:>5000" --from 24h
  ddx rum events --query "@application.name:\"1000Farmacie\" @type:error" --from 4h
  ddx rum events --query "@user.id:123" --from 7d --detailed
  ddx rum events --query "@type:resource @view.url:*/checkout/*" --from 1h`,
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
			"filter": map[string]any{
				"query": rumQuery,
				"from":  fmt.Sprintf("%d000", from),
				"to":    fmt.Sprintf("%d000", to),
			},
			"sort": "-timestamp",
			"page": map[string]any{
				"limit": limitFlag,
			},
		}

		data, err := c.Post(context.Background(), "api/v2/rum/events/search", body)
		if err != nil {
			return err
		}

		result := extractWithMeta(data, "rum")

		if verboseFlag || rumDetailed {
			explorerURL := buildExplorerURL("rum", rumQuery, from, to)
			fmt.Fprintln(cmd.ErrOrStderr(), "Explorer:", explorerURL)
		}

		return printData("", result)
	},
}

var rumSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List RUM sessions",
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
		params.Set("filter[query]", rumQuery)
		params.Set("filter[from]", strconv.FormatInt(from, 10)+"000")
		params.Set("filter[to]", strconv.FormatInt(to, 10)+"000")
		params.Set("page[limit]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/rum/events", params)
		if err != nil {
			return err
		}

		return printData("", extractWithMeta(data, "rum sessions"))
	},
}

var rumAggregateCmd = &cobra.Command{
	Use:   "aggregate",
	Short: "Aggregate RUM events with server-side grouping",
	Long: `Aggregate RUM events with server-side compute and grouping.

Examples:
  ddx rum aggregate --query "@type:error" --compute "count" --group-by "@view.url" --from 1h
  ddx rum aggregate --query "@type:view" --compute "avg(@view.loading_time)" --group-by "@session.type" --from 4h`,
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

		body := rumlogsBuildAggregateBody(rumlogsAggQuery, rumlogsAggCompute, rumlogsAggGroupBy, from, to, limitFlag)

		data, err := c.Post(context.Background(), "api/v2/rum/analytics/aggregate", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

// rumlogsBuildAggregateBody builds the request body for POST
// api/v2/rum/analytics/aggregate. It mirrors logsAggregateCmd's compute
// parsing ("count", "avg(@field)", …) and group-by shape exactly, so the two
// aggregate commands behave identically from the user's perspective.
func rumlogsBuildAggregateBody(query, compute, groupBy string, from, to int64, limit int) map[string]any {
	computeObj := map[string]any{
		"aggregation": compute,
	}
	// Handle "avg(@field)" style — extract aggregation + metric
	if idx := indexOf(compute, '('); idx > 0 && compute[len(compute)-1] == ')' {
		computeObj["aggregation"] = compute[:idx]
		computeObj["metric"] = compute[idx+1 : len(compute)-1]
	}

	body := map[string]any{
		"filter": map[string]any{
			"query": query,
			"from":  fmt.Sprintf("%d000", from),
			"to":    fmt.Sprintf("%d000", to),
		},
		"compute": []map[string]any{computeObj},
	}

	if groupBy != "" {
		body["group_by"] = []map[string]any{
			{
				"facet": groupBy,
				"limit": limit,
				"sort":  map[string]any{"order": "desc"},
			},
		}
	}

	return body
}

var rumRetentionFiltersCmd = &cobra.Command{
	Use:   "retention-filters",
	Short: "Manage RUM retention filters for a RUM application (scoped by --app)",
}

var rumRFListCmd = &cobra.Command{
	Use:   "list",
	Short: "List retention filters for a RUM application",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/rum/applications/"+rumlogsRFApp+"/retention_filters", nil)
		if err != nil {
			return err
		}
		return printData("", flattenV2Items(extractData(data)))
	},
}

var rumRFGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a RUM retention filter by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/rum/applications/"+rumlogsRFApp+"/retention_filters/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumRFCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a RUM retention filter",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create retention filter %q (event_type=%s) for app %s, no changes made\n", rumlogsRFName, rumlogsRFEventType, rumlogsRFApp)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating retention filter %q for RUM app %s", rumlogsRFName, rumlogsRFApp)); err != nil {
			return err
		}

		f := rumlogsRetentionFilterFields{
			Name:          rumlogsRFName,
			NameSet:       true,
			EventType:     rumlogsRFEventType,
			EventTypeSet:  true,
			Query:         rumlogsRFQuery,
			QuerySet:      cmd.Flags().Changed("query"),
			SampleRate:    rumlogsRFSampleRate,
			SampleRateSet: true,
			Enabled:       rumlogsRFEnabled,
			EnabledSet:    cmd.Flags().Changed("enabled"),
		}
		body := rumlogsRetentionFilterCreateBody(f)

		data, err := c.Post(context.Background(), "api/v2/rum/applications/"+rumlogsRFApp+"/retention_filters", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumRFUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a RUM retention filter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would update retention filter %s for app %s, no changes made\n", args[0], rumlogsRFApp)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("updating retention filter %s for RUM app %s", args[0], rumlogsRFApp)); err != nil {
			return err
		}

		f := rumlogsRetentionFilterFields{
			Name:          rumlogsRFName,
			NameSet:       cmd.Flags().Changed("name"),
			EventType:     rumlogsRFEventType,
			EventTypeSet:  cmd.Flags().Changed("event-type"),
			Query:         rumlogsRFQuery,
			QuerySet:      cmd.Flags().Changed("query"),
			SampleRate:    rumlogsRFSampleRate,
			SampleRateSet: cmd.Flags().Changed("sample-rate"),
			Enabled:       rumlogsRFEnabled,
			EnabledSet:    cmd.Flags().Changed("enabled"),
		}
		body := rumlogsRetentionFilterUpdateBody(args[0], f)

		data, err := c.Patch(context.Background(), "api/v2/rum/applications/"+rumlogsRFApp+"/retention_filters/"+args[0], body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumRFDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a RUM retention filter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete retention filter %s for app %s, no changes made\n", args[0], rumlogsRFApp)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting retention filter %s for RUM app %s", args[0], rumlogsRFApp)); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/rum/applications/"+rumlogsRFApp+"/retention_filters/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Retention filter %s deleted\n", args[0])
		}
		return nil
	},
}

// rumlogsRetentionFilterFields carries the flag values for a retention-filter
// create/update request, plus whether each was explicitly set by the caller.
// create marks the spec-required fields (name/event-type/sample-rate) always
// set; update leaves that decision to cobra's Flags().Changed so a PATCH only
// ever sends the attributes the operator actually passed.
type rumlogsRetentionFilterFields struct {
	Name          string
	NameSet       bool
	EventType     string
	EventTypeSet  bool
	Query         string
	QuerySet      bool
	SampleRate    float64
	SampleRateSet bool
	Enabled       bool
	EnabledSet    bool
}

func rumlogsRetentionFilterAttrs(f rumlogsRetentionFilterFields) map[string]any {
	attrs := map[string]any{}
	if f.NameSet {
		attrs["name"] = f.Name
	}
	if f.EventTypeSet {
		attrs["event_type"] = f.EventType
	}
	if f.QuerySet {
		attrs["query"] = f.Query
	}
	if f.SampleRateSet {
		attrs["sample_rate"] = f.SampleRate
	}
	if f.EnabledSet {
		attrs["enabled"] = f.Enabled
	}
	return attrs
}

func rumlogsRetentionFilterCreateBody(f rumlogsRetentionFilterFields) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type":       "retention_filters",
			"attributes": rumlogsRetentionFilterAttrs(f),
		},
	}
}

func rumlogsRetentionFilterUpdateBody(id string, f rumlogsRetentionFilterFields) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":         id,
			"type":       "retention_filters",
			"attributes": rumlogsRetentionFilterAttrs(f),
		},
	}
}

var rumMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Manage RUM-based metrics (api/v2/rum/config/metrics)",
}

var rumMetricsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List RUM-based metrics",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/rum/config/metrics", nil)
		if err != nil {
			return err
		}
		return printData("", flattenV2Items(extractData(data)))
	},
}

var rumMetricsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a RUM-based metric by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/rum/config/metrics/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumMetricsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a RUM-based metric",
	Long: `Create a metric based on your organization's RUM data.

Examples:
  ddx rum metrics create --id rum.sessions.web.count --event-type session --compute count
  ddx rum metrics create --id rum.view.duration --event-type view --compute "distribution/@duration" --include-percentiles --group-by "@browser.name:browser_name"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create RUM metric %q (event_type=%s), no changes made\n", rumlogsRMID, rumlogsRMEventType)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating RUM metric %q", rumlogsRMID)); err != nil {
			return err
		}

		aggType, path, err := rumlogsParseCompute(rumlogsRMCompute)
		if err != nil {
			return err
		}

		f := rumlogsRumMetricFields{
			ID:                    rumlogsRMID,
			EventType:             rumlogsRMEventType,
			AggregationType:       aggType,
			Path:                  path,
			PathSet:               path != "",
			IncludePercentiles:    rumlogsRMIncludePercentiles,
			IncludePercentilesSet: cmd.Flags().Changed("include-percentiles"),
			Query:                 rumlogsRMQuery,
			QuerySet:              cmd.Flags().Changed("query"),
			GroupBy:               rumlogsParseGroupBy(rumlogsRMGroupBy),
			UniquenessWhen:        rumlogsRMUniquenessWhen,
			UniquenessSet:         cmd.Flags().Changed("uniqueness-when"),
		}
		body := rumlogsRumMetricCreateBody(f)

		data, err := c.Post(context.Background(), "api/v2/rum/config/metrics", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumMetricsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a RUM-based metric",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete RUM metric %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting RUM metric %s", args[0])); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/rum/config/metrics/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "RUM metric %s deleted\n", args[0])
		}
		return nil
	},
}

// rumlogsRumMetricFields carries the flag values for a RUM-based metric
// create request, plus whether each optional field was explicitly set.
type rumlogsRumMetricFields struct {
	ID                    string
	EventType             string
	AggregationType       string
	Path                  string
	PathSet               bool
	IncludePercentiles    bool
	IncludePercentilesSet bool
	Query                 string
	QuerySet              bool
	GroupBy               []map[string]any
	UniquenessWhen        string
	UniquenessSet         bool
}

func rumlogsRumMetricCreateBody(f rumlogsRumMetricFields) map[string]any {
	compute := map[string]any{"aggregation_type": f.AggregationType}
	if f.PathSet {
		compute["path"] = f.Path
	}
	if f.IncludePercentilesSet {
		compute["include_percentiles"] = f.IncludePercentiles
	}

	attrs := map[string]any{
		"event_type": f.EventType,
		"compute":    compute,
	}
	if f.QuerySet {
		attrs["filter"] = map[string]any{"query": f.Query}
	}
	if len(f.GroupBy) > 0 {
		attrs["group_by"] = f.GroupBy
	}
	if f.UniquenessSet {
		attrs["uniqueness"] = map[string]any{"when": f.UniquenessWhen}
	}

	return map[string]any{
		"data": map[string]any{
			"id":         f.ID,
			"type":       "rum_metrics",
			"attributes": attrs,
		},
	}
}

// rumlogsParseCompute parses the shared compute mini-language used by both
// "rum metrics create" and "logs metrics create": either a bare aggregation
// type ("count") or "<aggregation_type>/<path>" ("distribution/@duration").
func rumlogsParseCompute(spec string) (aggType, path string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("--compute is required")
	}
	if idx := strings.IndexByte(spec, '/'); idx >= 0 {
		return spec[:idx], spec[idx+1:], nil
	}
	return spec, "", nil
}

// rumlogsParseGroupBy parses repeatable "--group-by" values shared by "rum
// metrics create" and "logs metrics create" into the {path, tag_name} objects
// the RUM/logs metrics config APIs expect. Each entry is either a bare facet
// path ("@browser.name") or "path:tag_name" ("@browser.name:browser_name").
func rumlogsParseGroupBy(specs []string) []map[string]any {
	var out []map[string]any
	for _, s := range specs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		g := map[string]any{}
		if idx := strings.IndexByte(s, ':'); idx >= 0 {
			g["path"] = s[:idx]
			g["tag_name"] = s[idx+1:]
		} else {
			g["path"] = s
		}
		out = append(out, g)
	}
	return out
}
