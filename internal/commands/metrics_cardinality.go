package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

// Custom-metrics cost-control suite: cardinality inspection (volumes,
// estimate, tag-cardinalities), tag configuration management, and the
// tag-indexing-rules family (the designated successor to the deprecated
// config/bulk-tags endpoint — see api/v2/metrics/config/bulk-tags in the
// spec). All package-level identifiers here carry the "cardinality" prefix
// to avoid collisions with the rest of the shared commands package.

var (
	cardinalityVolumesWindowSeconds int

	cardinalityEstimateGroups          string
	cardinalityEstimateHoursAgo        int
	cardinalityEstimateNumAggregations int
	cardinalityEstimatePct             bool
	cardinalityEstimateTimespanH       int

	cardinalityTagsSetMetricType         string
	cardinalityTagsSetTags               string
	cardinalityTagsSetExcludeTagsMode    bool
	cardinalityTagsSetIncludePercentiles bool

	cardinalityRuleListSearch string
	cardinalityRuleListOffset int

	cardinalityRuleCreateName            string
	cardinalityRuleCreateMetricMatches   string
	cardinalityRuleCreateIgnoredMatches  string
	cardinalityRuleCreateTags            string
	cardinalityRuleCreateExcludeTagsMode bool

	cardinalityRuleUpdateName            string
	cardinalityRuleUpdateMetricMatches   string
	cardinalityRuleUpdateIgnoredMatches  string
	cardinalityRuleUpdateTags            string
	cardinalityRuleUpdateExcludeTagsMode bool
	cardinalityRuleUpdateRuleOrder       int
)

func init() {
	metricsCmd.AddCommand(cardinalityVolumesCmd)
	metricsCmd.AddCommand(cardinalityEstimateCmd)
	metricsCmd.AddCommand(cardinalityTagCardinalitiesCmd)
	metricsCmd.AddCommand(cardinalityTagsCmd)
	metricsCmd.AddCommand(cardinalityTagRulesCmd)

	cardinalityTagsCmd.AddCommand(cardinalityTagsGetCmd)
	cardinalityTagsCmd.AddCommand(cardinalityTagsSetCmd)
	cardinalityTagsCmd.AddCommand(cardinalityTagsDeleteCmd)

	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesListCmd)
	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesGetCmd)
	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesCreateCmd)
	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesUpdateCmd)
	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesDeleteCmd)
	cardinalityTagRulesCmd.AddCommand(cardinalityTagRulesReorderCmd)

	cardinalityVolumesCmd.Flags().IntVar(&cardinalityVolumesWindowSeconds, "window", 0, "Look-back window in seconds (default 3600, max 2592000)")

	cardinalityEstimateCmd.Flags().StringVar(&cardinalityEstimateGroups, "groups", "", "Comma-separated tag keys the metric is queried with (e.g., app,host)")
	cardinalityEstimateCmd.Flags().IntVar(&cardinalityEstimateHoursAgo, "hours-ago", 0, "Hours of look-back (from now) to estimate cardinality with")
	cardinalityEstimateCmd.Flags().IntVar(&cardinalityEstimateNumAggregations, "num-aggregations", 0, "Deprecated: number of aggregations has no impact on volume")
	cardinalityEstimateCmd.Flags().BoolVar(&cardinalityEstimatePct, "pct", false, "Estimate cardinality including percentile aggregators (distribution metrics only)")
	cardinalityEstimateCmd.Flags().IntVar(&cardinalityEstimateTimespanH, "timespan-h", 0, "Look-back window in hours (minimum and default is 1 hour)")

	cardinalityTagsSetCmd.Flags().StringVar(&cardinalityTagsSetMetricType, "metric-type", "", "Metric type: gauge, count, rate, or distribution (required)")
	cardinalityTagsSetCmd.Flags().StringVar(&cardinalityTagsSetTags, "tags", "", "Comma-separated tag keys to make queryable (required)")
	cardinalityTagsSetCmd.Flags().BoolVar(&cardinalityTagsSetExcludeTagsMode, "exclude-tags-mode", false, "Treat --tags as a deny-list instead of an allow-list")
	cardinalityTagsSetCmd.Flags().BoolVar(&cardinalityTagsSetIncludePercentiles, "include-percentiles", false, "Include percentile aggregations (distribution metrics only)")
	cardinalityTagsSetCmd.MarkFlagRequired("metric-type")
	cardinalityTagsSetCmd.MarkFlagRequired("tags")

	cardinalityTagRulesListCmd.Flags().StringVar(&cardinalityRuleListSearch, "search", "", "Substring filter on rule name")
	cardinalityTagRulesListCmd.Flags().IntVar(&cardinalityRuleListOffset, "offset", 0, "Page offset from the start of the list")

	cardinalityTagRulesCreateCmd.Flags().StringVar(&cardinalityRuleCreateName, "name", "", "Human-readable rule name (required)")
	cardinalityTagRulesCreateCmd.Flags().StringVar(&cardinalityRuleCreateMetricMatches, "metric-name-matches", "", "Comma-separated metric name glob patterns this rule applies to (required)")
	cardinalityTagRulesCreateCmd.Flags().StringVar(&cardinalityRuleCreateIgnoredMatches, "ignored-metric-name-matches", "", "Comma-separated metric name prefixes excluded from the rule's scope")
	cardinalityTagRulesCreateCmd.Flags().StringVar(&cardinalityRuleCreateTags, "tags", "", "Comma-separated tag keys managed by this rule")
	cardinalityTagRulesCreateCmd.Flags().BoolVar(&cardinalityRuleCreateExcludeTagsMode, "exclude-tags-mode", false, "Exclude the listed tags and index all others, instead of only indexing the listed tags")
	cardinalityTagRulesCreateCmd.MarkFlagRequired("name")
	cardinalityTagRulesCreateCmd.MarkFlagRequired("metric-name-matches")

	cardinalityTagRulesUpdateCmd.Flags().StringVar(&cardinalityRuleUpdateName, "name", "", "Human-readable rule name")
	cardinalityTagRulesUpdateCmd.Flags().StringVar(&cardinalityRuleUpdateMetricMatches, "metric-name-matches", "", "Comma-separated metric name glob patterns this rule applies to")
	cardinalityTagRulesUpdateCmd.Flags().StringVar(&cardinalityRuleUpdateIgnoredMatches, "ignored-metric-name-matches", "", "Comma-separated metric name prefixes excluded from the rule's scope")
	cardinalityTagRulesUpdateCmd.Flags().StringVar(&cardinalityRuleUpdateTags, "tags", "", "Comma-separated tag keys managed by this rule")
	cardinalityTagRulesUpdateCmd.Flags().BoolVar(&cardinalityRuleUpdateExcludeTagsMode, "exclude-tags-mode", false, "Exclude the listed tags and index all others, instead of only indexing the listed tags")
	cardinalityTagRulesUpdateCmd.Flags().IntVar(&cardinalityRuleUpdateRuleOrder, "rule-order", 0, "Desired evaluation order (409 on conflict; use 'reorder' for atomic re-sequencing)")
}

var cardinalityVolumesCmd = &cobra.Command{
	Use:   "volumes <metric>",
	Short: "Hourly ingested/indexed volume for a custom metric",
	Long: `View hourly average cardinality (or, for Metric Name Pricing customers,
total point volume) for the given metric name over the look-back period.

Examples:
  ddx metrics volumes system.cpu.user
  ddx metrics volumes system.cpu.user --window 7200`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if cardinalityVolumesWindowSeconds > 0 {
			params.Set("window[seconds]", strconv.Itoa(cardinalityVolumesWindowSeconds))
		}

		data, err := c.Get(context.Background(), "api/v2/metrics/"+url.PathEscape(args[0])+"/volumes", params)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var cardinalityEstimateCmd = &cobra.Command{
	Use:   "estimate <metric>",
	Short: "Estimate output-series cardinality for a tag/percentile configuration",
	Long: `Returns the estimated cardinality for a metric with a given tag,
percentile, and aggregation configuration using Metrics without Limits(tm).

Examples:
  ddx metrics estimate system.cpu.user --groups app,host
  ddx metrics estimate dist.request.latency --groups app --pct --hours-ago 49`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := cardinalityBuildEstimateParams(
			cardinalityEstimateGroups,
			cardinalityEstimateHoursAgo,
			cardinalityEstimateNumAggregations,
			cardinalityEstimateTimespanH,
			cardinalityEstimatePct,
		)

		data, err := c.Get(context.Background(), "api/v2/metrics/"+url.PathEscape(args[0])+"/estimate", params)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

// cardinalityBuildEstimateParams builds the filter[...] query params for the
// estimate endpoint. Zero-valued ints/false bools are omitted (equivalent to
// "not requested" for this read-only endpoint — the API applies its own
// defaults when a filter is absent).
func cardinalityBuildEstimateParams(groups string, hoursAgo, numAggregations, timespanH int, pct bool) url.Values {
	params := url.Values{}
	if groups != "" {
		params.Set("filter[groups]", groups)
	}
	if hoursAgo != 0 {
		params.Set("filter[hours_ago]", strconv.Itoa(hoursAgo))
	}
	if numAggregations != 0 {
		params.Set("filter[num_aggregations]", strconv.Itoa(numAggregations))
	}
	if timespanH != 0 {
		params.Set("filter[timespan_h]", strconv.Itoa(timespanH))
	}
	if pct {
		params.Set("filter[pct]", "true")
	}
	return params
}

var cardinalityTagCardinalitiesCmd = &cobra.Command{
	Use:   "tag-cardinalities <metric>",
	Short: "Per-tag cardinality deltas for a custom metric",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/metrics/"+url.PathEscape(args[0])+"/tag-cardinalities", nil)
		if err != nil {
			return err
		}
		return printData("metrics.tag-cardinalities", flattenV2Items(extractData(data)))
	},
}

var cardinalityTagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Read and manage a custom metric's tag configuration",
}

var cardinalityTagsGetCmd = &cobra.Command{
	Use:   "get <metric>",
	Short: "Get a metric's tag configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/metrics/"+url.PathEscape(args[0])+"/tags", nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var cardinalityValidMetricTypes = []string{"gauge", "count", "rate", "distribution"}

// cardinalityValidateMetricType checks s against the enum the spec defines
// for MetricTagConfigurationMetricTypes (gauge, count, rate, distribution).
func cardinalityValidateMetricType(s string) error {
	for _, v := range cardinalityValidMetricTypes {
		if v == s {
			return nil
		}
	}
	return fmt.Errorf("invalid --metric-type %q: must be one of gauge, count, rate, distribution", s)
}

// cardinalityBuildTagConfigBody builds the {"data": {"type": "manage_tags",
// "id": ..., "attributes": {...}}} body shared by create (POST) and update
// (PATCH) of a metric's tag configuration. A nil pointer omits that
// attribute from the request so PATCH's "omitted fields are left unchanged"
// semantics are preserved; metricType is nil on every PATCH call since the
// update schema doesn't accept it (immutable after creation).
func cardinalityBuildTagConfigBody(metricName string, metricType *string, tags *[]string, excludeTagsMode, includePercentiles *bool) map[string]any {
	attrs := map[string]any{}
	if metricType != nil {
		attrs["metric_type"] = *metricType
	}
	if tags != nil {
		attrs["tags"] = *tags
	}
	if excludeTagsMode != nil {
		attrs["exclude_tags_mode"] = *excludeTagsMode
	}
	if includePercentiles != nil {
		attrs["include_percentiles"] = *includePercentiles
	}
	return map[string]any{
		"data": map[string]any{
			"type":       "manage_tags",
			"id":         metricName,
			"attributes": attrs,
		},
	}
}

var cardinalityTagsSetCmd = &cobra.Command{
	Use:   "set <metric>",
	Short: "Create or update a metric's tag configuration",
	Long: `Create-or-update: tries to create the tag configuration first; if one
already exists (409), falls back to a partial update with the same tags/mode
(metric_type can't be changed once a configuration exists).

Examples:
  ddx metrics tags set http.endpoint.request --metric-type distribution --tags app,datacenter
  ddx metrics tags set http.endpoint.request --metric-type distribution --tags app --exclude-tags-mode --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		metric := args[0]
		tags := splitComma(cardinalityTagsSetTags)

		if err := cardinalityValidateMetricType(cardinalityTagsSetMetricType); err != nil {
			return err
		}

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would set tag configuration for metric %q (metric_type=%s, tags=%v), no changes made\n", metric, cardinalityTagsSetMetricType, tags)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("setting tag configuration for metric %q", metric)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		excludeTagsMode := cmd.Flags().Changed("exclude-tags-mode")
		includePercentiles := cmd.Flags().Changed("include-percentiles")
		var excludePtr, percentilesPtr *bool
		if excludeTagsMode {
			v := cardinalityTagsSetExcludeTagsMode
			excludePtr = &v
		}
		if includePercentiles {
			v := cardinalityTagsSetIncludePercentiles
			percentilesPtr = &v
		}

		metricType := cardinalityTagsSetMetricType
		path := "api/v2/metrics/" + url.PathEscape(metric) + "/tags"

		createBody := cardinalityBuildTagConfigBody(metric, &metricType, &tags, excludePtr, percentilesPtr)
		data, err := c.Post(context.Background(), path, createBody)
		if err != nil {
			if !cardinalityIsConflict(err) {
				return err
			}
			// Tag configuration already exists — fall back to a partial
			// update. metric_type is omitted: it's immutable post-creation.
			updateBody := cardinalityBuildTagConfigBody(metric, nil, &tags, excludePtr, percentilesPtr)
			data, err = c.Patch(context.Background(), path, updateBody)
			if err != nil {
				return err
			}
		}
		return printData("", data)
	},
}

var cardinalityTagsDeleteCmd = &cobra.Command{
	Use:   "delete <metric>",
	Short: "Delete a metric's tag configuration (irreversible)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		metric := args[0]
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete tag configuration for metric %q, no changes made\n", metric)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting tag configuration for metric %q", metric)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/metrics/"+url.PathEscape(metric)+"/tags"); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Tag configuration for metric %q deleted\n", metric)
		}
		return nil
	},
}

// cardinalityIsConflict reports whether err is a Datadog 409 response,
// distinguishing "tag configuration already exists" from a real failure.
func cardinalityIsConflict(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 409
}

// --- tag-indexing-rules: successor to the deprecated config/bulk-tags. ---

var cardinalityTagRulesCmd = &cobra.Command{
	Use:   "tag-rules",
	Short: "Manage org-wide tag indexing rules (successor to bulk-tags)",
	Long: `Tag indexing rules are the designated successor to the deprecated
config/bulk-tags endpoint. Preview/Unstable per the Datadog API spec.`,
}

var cardinalityTagRulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tag indexing rules, sorted by rule_order",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if limitFlag > 0 {
			params.Set("page[limit]", strconv.Itoa(limitFlag))
		}
		if cardinalityRuleListOffset > 0 {
			params.Set("page[offset]", strconv.Itoa(cardinalityRuleListOffset))
		}
		if cardinalityRuleListSearch != "" {
			params.Set("search", cardinalityRuleListSearch)
		}

		data, err := c.Get(context.Background(), "api/v2/metrics/tag-indexing-rules", params)
		if err != nil {
			return err
		}
		return printData("metrics.tag-rules.list", flattenV2Items(extractData(data)))
	},
}

var cardinalityTagRulesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a tag indexing rule by UUID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		data, err := c.Get(context.Background(), "api/v2/metrics/tag-indexing-rules/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

// cardinalityBuildTagIndexingRuleBody builds the {"data": {"type":
// "tag_indexing_rules", "attributes": {...}}} body shared by create (POST)
// and update (PUT). A nil pointer omits that attribute; PUT's "fields
// omitted from the request body are left unchanged" semantics rely on this.
func cardinalityBuildTagIndexingRuleBody(name *string, metricNameMatches, ignoredMetricNameMatches, tags *[]string, excludeTagsMode *bool, ruleOrder *int) map[string]any {
	attrs := map[string]any{}
	if name != nil {
		attrs["name"] = *name
	}
	if metricNameMatches != nil {
		attrs["metric_name_matches"] = *metricNameMatches
	}
	if ignoredMetricNameMatches != nil {
		attrs["ignored_metric_name_matches"] = *ignoredMetricNameMatches
	}
	if tags != nil {
		attrs["tags"] = *tags
	}
	if excludeTagsMode != nil {
		attrs["exclude_tags_mode"] = *excludeTagsMode
	}
	if ruleOrder != nil {
		attrs["rule_order"] = *ruleOrder
	}
	return map[string]any{
		"data": map[string]any{
			"type":       "tag_indexing_rules",
			"attributes": attrs,
		},
	}
}

var cardinalityTagRulesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a tag indexing rule",
	Long: `rule_order is assigned server-side as max+1 among existing rules;
use "tag-rules reorder" to change the evaluation order afterwards.

Example:
  ddx metrics tag-rules create --name my-rule --metric-name-matches "dd.test.*" --tags env,service --yes`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := cardinalityRuleCreateName
		metricNameMatches := splitComma(cardinalityRuleCreateMetricMatches)

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create tag indexing rule %q (metric_name_matches=%v), no changes made\n", name, metricNameMatches)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating tag indexing rule %q", name)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		var ignoredPtr, tagsPtr *[]string
		if cardinalityRuleCreateIgnoredMatches != "" {
			v := splitComma(cardinalityRuleCreateIgnoredMatches)
			ignoredPtr = &v
		}
		if cardinalityRuleCreateTags != "" {
			v := splitComma(cardinalityRuleCreateTags)
			tagsPtr = &v
		}
		var excludePtr *bool
		if cmd.Flags().Changed("exclude-tags-mode") {
			v := cardinalityRuleCreateExcludeTagsMode
			excludePtr = &v
		}

		body := cardinalityBuildTagIndexingRuleBody(&name, &metricNameMatches, ignoredPtr, tagsPtr, excludePtr, nil)
		data, err := c.Post(context.Background(), "api/v2/metrics/tag-indexing-rules", body)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var cardinalityTagRulesUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Partially update a tag indexing rule",
	Long: `All fields are optional; omitted flags leave the corresponding
attribute unchanged. Setting --rule-order to a value already used by another
rule returns 409 — use "tag-rules reorder" for atomic re-sequencing.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would update tag indexing rule %s, no changes made\n", id)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("updating tag indexing rule %s", id)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		var namePtr *string
		var metricMatchesPtr, ignoredPtr, tagsPtr *[]string
		var excludePtr *bool
		var ruleOrderPtr *int

		if cmd.Flags().Changed("name") {
			v := cardinalityRuleUpdateName
			namePtr = &v
		}
		if cmd.Flags().Changed("metric-name-matches") {
			v := splitComma(cardinalityRuleUpdateMetricMatches)
			metricMatchesPtr = &v
		}
		if cmd.Flags().Changed("ignored-metric-name-matches") {
			v := splitComma(cardinalityRuleUpdateIgnoredMatches)
			ignoredPtr = &v
		}
		if cmd.Flags().Changed("tags") {
			v := splitComma(cardinalityRuleUpdateTags)
			tagsPtr = &v
		}
		if cmd.Flags().Changed("exclude-tags-mode") {
			v := cardinalityRuleUpdateExcludeTagsMode
			excludePtr = &v
		}
		if cmd.Flags().Changed("rule-order") {
			v := cardinalityRuleUpdateRuleOrder
			ruleOrderPtr = &v
		}

		body := cardinalityBuildTagIndexingRuleBody(namePtr, metricMatchesPtr, ignoredPtr, tagsPtr, excludePtr, ruleOrderPtr)
		data, err := c.Put(context.Background(), "api/v2/metrics/tag-indexing-rules/"+id, body)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var cardinalityTagRulesDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Soft-delete a tag indexing rule",
	Long: `Idempotent: returns success whether the rule existed or was already
deleted. Remaining rules are automatically re-sequenced to keep rule_order
dense and 1-based.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete tag indexing rule %s, no changes made\n", id)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting tag indexing rule %s", id)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/metrics/tag-indexing-rules/"+id); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Tag indexing rule %s deleted\n", id)
		}
		return nil
	},
}

var cardinalityTagRulesReorderCmd = &cobra.Command{
	Use:   "reorder <id> [id...]",
	Short: "Atomically re-sequence tag indexing rules to match the given UUID order",
	Long: `Assigns rule_order 1, 2, ... matching each rule UUID's position in the
argument list.

Example:
  ddx metrics tag-rules reorder 00000000-0000-0000-0000-000000000002 00000000-0000-0000-0000-000000000001 --yes`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would reorder %d tag indexing rules to %v, no changes made\n", len(args), args)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("reordering %d tag indexing rules", len(args))); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		body := cardinalityBuildReorderBody(args)
		if _, err := c.Post(context.Background(), "api/v2/metrics/tag-indexing-rules/order", body); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Reordered %d tag indexing rules\n", len(args))
		}
		return nil
	},
}

// cardinalityBuildReorderBody builds the reorder request body: an ordered
// list of rule UUIDs, positionally assigned rule_order 1, 2, ... server-side.
func cardinalityBuildReorderBody(ids []string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type": "tag_indexing_rules",
			"attributes": map[string]any{
				"rule_ids": ids,
			},
		},
	}
}
