package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// rumlogs* — package-prefixed identifiers owned by this file plus rum.go,
// kept distinct from every other commands-package file to avoid collisions
// on merge. This file attaches new subcommands to the package-level logsCmd
// declared in logs.go — logs.go itself is never edited.
var (
	// logs metrics (api/v2/logs/config/metrics)
	rumlogsLMName               string
	rumlogsLMQuery              string
	rumlogsLMCompute            string
	rumlogsLMIncludePercentiles bool
	rumlogsLMGroupBy            []string

	// logs archives (api/v2/logs/config/archives)
	rumlogsArchName               string
	rumlogsArchQuery              string
	rumlogsArchDestJSON           string
	rumlogsArchCompressionMethod  string
	rumlogsArchIncludeTags        bool
	rumlogsArchRehydrationMaxScan int64
	rumlogsArchRehydrationTags    []string

	// logs custom destinations (api/v2/logs/config/custom-destinations)
	rumlogsDestName                    string
	rumlogsDestQuery                   string
	rumlogsDestEnabled                 bool
	rumlogsDestForwardTags             bool
	rumlogsDestForwardTagsRestrictList []string
	rumlogsDestForwardTagsRestrictType string
	rumlogsDestForwarderJSON           string
)

func init() {
	logsCmd.AddCommand(rumlogsLogsMetricsCmd)
	rumlogsLogsMetricsCmd.AddCommand(rumlogsLogsMetricsListCmd)
	rumlogsLogsMetricsCmd.AddCommand(rumlogsLogsMetricsGetCmd)
	rumlogsLogsMetricsCmd.AddCommand(rumlogsLogsMetricsCreateCmd)
	rumlogsLogsMetricsCmd.AddCommand(rumlogsLogsMetricsDeleteCmd)

	rumlogsLogsMetricsCreateCmd.Flags().StringVar(&rumlogsLMName, "name", "", "Log-based metric name, e.g. logs.page.load.count (required)")
	rumlogsLogsMetricsCreateCmd.Flags().StringVar(&rumlogsLMQuery, "query", "", "Log search query to filter on")
	rumlogsLogsMetricsCreateCmd.Flags().StringVar(&rumlogsLMCompute, "compute", "", `Compute spec: "count" or "<aggregation_type>/<path>", e.g. "distribution/@duration" (required)`)
	rumlogsLogsMetricsCreateCmd.Flags().BoolVar(&rumlogsLMIncludePercentiles, "include-percentiles", false, "Include percentile aggregations (distribution metrics only)")
	rumlogsLogsMetricsCreateCmd.Flags().StringSliceVar(&rumlogsLMGroupBy, "group-by", nil, `Group-by rule(s) "path[:tag_name]", repeatable`)
	rumlogsLogsMetricsCreateCmd.MarkFlagRequired("name")
	rumlogsLogsMetricsCreateCmd.MarkFlagRequired("compute")

	logsCmd.AddCommand(rumlogsArchivesCmd)
	rumlogsArchivesCmd.AddCommand(rumlogsArchivesListCmd)
	rumlogsArchivesCmd.AddCommand(rumlogsArchivesGetCmd)
	rumlogsArchivesCmd.AddCommand(rumlogsArchivesCreateCmd)
	rumlogsArchivesCmd.AddCommand(rumlogsArchivesUpdateCmd)
	rumlogsArchivesCmd.AddCommand(rumlogsArchivesDeleteCmd)

	for _, c := range []*cobra.Command{rumlogsArchivesCreateCmd, rumlogsArchivesUpdateCmd} {
		c.Flags().StringVar(&rumlogsArchName, "name", "", "Archive name (required)")
		c.Flags().StringVar(&rumlogsArchQuery, "query", "", "Archive query/filter — logs matching this query are included (required)")
		c.Flags().StringVar(&rumlogsArchDestJSON, "destination-json", "", `Raw JSON for the destination object (s3/gcs/azure union), e.g. '{"type":"s3","bucket":"my-bucket","integration":{"account_id":"123","role_name":"my-role"}}' (required)`)
		c.Flags().StringVar(&rumlogsArchCompressionMethod, "compression-method", "", "Compression method: GZIP or ZSTD")
		c.Flags().BoolVar(&rumlogsArchIncludeTags, "include-tags", false, "Store tags in the archive (default: tags are stripped)")
		c.Flags().Int64Var(&rumlogsArchRehydrationMaxScan, "rehydration-max-scan-gb", 0, "Maximum scan size in GB for rehydration from this archive")
		c.Flags().StringSliceVar(&rumlogsArchRehydrationTags, "rehydration-tags", nil, "Tags to add to rehydrated logs, e.g. team:intake")
		c.MarkFlagRequired("name")
		c.MarkFlagRequired("query")
		c.MarkFlagRequired("destination-json")
	}

	logsCmd.AddCommand(rumlogsDestinationsCmd)
	rumlogsDestinationsCmd.AddCommand(rumlogsDestinationsListCmd)
	rumlogsDestinationsCmd.AddCommand(rumlogsDestinationsGetCmd)
	rumlogsDestinationsCmd.AddCommand(rumlogsDestinationsCreateCmd)
	rumlogsDestinationsCmd.AddCommand(rumlogsDestinationsUpdateCmd)
	rumlogsDestinationsCmd.AddCommand(rumlogsDestinationsDeleteCmd)

	rumlogsDestinationsCreateCmd.Flags().StringVar(&rumlogsDestName, "name", "", "Custom destination name (required)")
	rumlogsDestinationsCreateCmd.Flags().StringVar(&rumlogsDestQuery, "query", "", "Query/filter — logs matching this query are forwarded")
	rumlogsDestinationsCreateCmd.Flags().BoolVar(&rumlogsDestEnabled, "enabled", true, "Whether matching logs should be forwarded")
	rumlogsDestinationsCreateCmd.Flags().BoolVar(&rumlogsDestForwardTags, "forward-tags", true, "Whether tags from forwarded logs should be forwarded")
	rumlogsDestinationsCreateCmd.Flags().StringSliceVar(&rumlogsDestForwardTagsRestrictList, "forward-tags-restriction-list", nil, "Tag keys to filter, e.g. datacenter,host")
	rumlogsDestinationsCreateCmd.Flags().StringVar(&rumlogsDestForwardTagsRestrictType, "forward-tags-restriction-list-type", "", "ALLOW_LIST or BLOCK_LIST")
	rumlogsDestinationsCreateCmd.Flags().StringVar(&rumlogsDestForwarderJSON, "forwarder-json", "", `Raw JSON for forwarder_destination (http/splunk/elasticsearch/microsoft_sentinel union), e.g. '{"type":"http","endpoint":"https://example.com","auth":{"type":"basic","username":"u","password":"p"}}' (required)`)
	rumlogsDestinationsCreateCmd.MarkFlagRequired("name")
	rumlogsDestinationsCreateCmd.MarkFlagRequired("forwarder-json")

	rumlogsDestinationsUpdateCmd.Flags().StringVar(&rumlogsDestName, "name", "", "Custom destination name")
	rumlogsDestinationsUpdateCmd.Flags().StringVar(&rumlogsDestQuery, "query", "", "Query/filter — logs matching this query are forwarded")
	rumlogsDestinationsUpdateCmd.Flags().BoolVar(&rumlogsDestEnabled, "enabled", true, "Whether matching logs should be forwarded")
	rumlogsDestinationsUpdateCmd.Flags().BoolVar(&rumlogsDestForwardTags, "forward-tags", true, "Whether tags from forwarded logs should be forwarded")
	rumlogsDestinationsUpdateCmd.Flags().StringSliceVar(&rumlogsDestForwardTagsRestrictList, "forward-tags-restriction-list", nil, "Tag keys to filter, e.g. datacenter,host")
	rumlogsDestinationsUpdateCmd.Flags().StringVar(&rumlogsDestForwardTagsRestrictType, "forward-tags-restriction-list-type", "", "ALLOW_LIST or BLOCK_LIST")
	rumlogsDestinationsUpdateCmd.Flags().StringVar(&rumlogsDestForwarderJSON, "forwarder-json", "", "Raw JSON for forwarder_destination (http/splunk/elasticsearch/microsoft_sentinel union)")
}

// ---------------------------------------------------------------------------
// logs metrics — api/v2/logs/config/metrics
// ---------------------------------------------------------------------------

var rumlogsLogsMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Manage log-based metrics (api/v2/logs/config/metrics)",
}

var rumlogsLogsMetricsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List log-based metrics",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/metrics", nil)
		if err != nil {
			return err
		}
		return printData("", flattenV2Items(extractData(data)))
	},
}

var rumlogsLogsMetricsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a log-based metric by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/metrics/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsLogsMetricsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a log-based metric",
	Long: `Create a metric based on your ingested logs. This is the prerequisite for
turning expensive "count bot hits" queries into cheap metrics, cutting
indexed log volume.

Examples:
  ddx logs metrics create --name logs.bot.count --query "@user_agent.type:crawler" --compute count
  ddx logs metrics create --name logs.page.load.count --query "service:web*" --compute "distribution/@duration" --group-by "@http.status_code:status_code"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create log-based metric %q, no changes made\n", rumlogsLMName)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating log-based metric %q", rumlogsLMName)); err != nil {
			return err
		}

		aggType, path, err := rumlogsParseCompute(rumlogsLMCompute)
		if err != nil {
			return err
		}

		f := rumlogsLogsMetricFields{
			ID:                    rumlogsLMName,
			AggregationType:       aggType,
			Path:                  path,
			PathSet:               path != "",
			IncludePercentiles:    rumlogsLMIncludePercentiles,
			IncludePercentilesSet: cmd.Flags().Changed("include-percentiles"),
			Query:                 rumlogsLMQuery,
			QuerySet:              cmd.Flags().Changed("query"),
			GroupBy:               rumlogsParseGroupBy(rumlogsLMGroupBy),
		}
		body := rumlogsLogsMetricCreateBody(f)

		data, err := c.Post(context.Background(), "api/v2/logs/config/metrics", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsLogsMetricsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a log-based metric",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete log-based metric %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting log-based metric %s", args[0])); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/logs/config/metrics/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Log-based metric %s deleted\n", args[0])
		}
		return nil
	},
}

// rumlogsLogsMetricFields carries the flag values for a log-based metric
// create request, plus whether each optional field was explicitly set.
type rumlogsLogsMetricFields struct {
	ID                    string
	AggregationType       string
	Path                  string
	PathSet               bool
	IncludePercentiles    bool
	IncludePercentilesSet bool
	Query                 string
	QuerySet              bool
	GroupBy               []map[string]any
}

func rumlogsLogsMetricCreateBody(f rumlogsLogsMetricFields) map[string]any {
	compute := map[string]any{"aggregation_type": f.AggregationType}
	if f.PathSet {
		compute["path"] = f.Path
	}
	if f.IncludePercentilesSet {
		compute["include_percentiles"] = f.IncludePercentiles
	}

	attrs := map[string]any{"compute": compute}
	if f.QuerySet {
		attrs["filter"] = map[string]any{"query": f.Query}
	}
	if len(f.GroupBy) > 0 {
		attrs["group_by"] = f.GroupBy
	}

	return map[string]any{
		"data": map[string]any{
			"id":         f.ID,
			"type":       "logs_metrics",
			"attributes": attrs,
		},
	}
}

// ---------------------------------------------------------------------------
// logs archives — api/v2/logs/config/archives
// ---------------------------------------------------------------------------

var rumlogsArchivesCmd = &cobra.Command{
	Use:   "archives",
	Short: "Manage logs archives (api/v2/logs/config/archives)",
}

var rumlogsArchivesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List logs archives",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/archives", nil)
		if err != nil {
			return err
		}
		return printData("", flattenV2Items(extractData(data)))
	},
}

var rumlogsArchivesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a logs archive by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/archives/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsArchivesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a logs archive",
	Long: `Create a logs archive. The destination (S3/GCS/Azure union) is passed as raw
JSON via --destination-json rather than modeled flag-by-flag.

Examples:
  ddx logs archives create --name "Nginx Archive" --query "source:nginx" \
    --destination-json '{"type":"s3","bucket":"my-bucket","integration":{"account_id":"123456789012","role_name":"my-role"}}'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create logs archive %q, no changes made\n", rumlogsArchName)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating logs archive %q", rumlogsArchName)); err != nil {
			return err
		}

		dest, err := rumlogsParseDestinationJSON(rumlogsArchDestJSON)
		if err != nil {
			return err
		}
		f := rumlogsArchiveFields{
			Name:                    rumlogsArchName,
			Query:                   rumlogsArchQuery,
			Destination:             dest,
			CompressionMethod:       rumlogsArchCompressionMethod,
			CompressionMethodSet:    cmd.Flags().Changed("compression-method"),
			IncludeTags:             rumlogsArchIncludeTags,
			IncludeTagsSet:          cmd.Flags().Changed("include-tags"),
			RehydrationMaxScanGB:    rumlogsArchRehydrationMaxScan,
			RehydrationMaxScanGBSet: cmd.Flags().Changed("rehydration-max-scan-gb"),
			RehydrationTags:         rumlogsArchRehydrationTags,
		}
		body := rumlogsArchiveBody(f)

		data, err := c.Post(context.Background(), "api/v2/logs/config/archives", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsArchivesUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a logs archive (full replacement)",
	Long: `Update a logs archive. Note this is a PUT — it REPLACES the archive's full
configuration, it does not merge. Pass every attribute you want to keep.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would replace logs archive %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("replacing logs archive %s", args[0])); err != nil {
			return err
		}

		dest, err := rumlogsParseDestinationJSON(rumlogsArchDestJSON)
		if err != nil {
			return err
		}
		f := rumlogsArchiveFields{
			Name:                    rumlogsArchName,
			Query:                   rumlogsArchQuery,
			Destination:             dest,
			CompressionMethod:       rumlogsArchCompressionMethod,
			CompressionMethodSet:    cmd.Flags().Changed("compression-method"),
			IncludeTags:             rumlogsArchIncludeTags,
			IncludeTagsSet:          cmd.Flags().Changed("include-tags"),
			RehydrationMaxScanGB:    rumlogsArchRehydrationMaxScan,
			RehydrationMaxScanGBSet: cmd.Flags().Changed("rehydration-max-scan-gb"),
			RehydrationTags:         rumlogsArchRehydrationTags,
		}
		body := rumlogsArchiveBody(f)

		data, err := c.Put(context.Background(), "api/v2/logs/config/archives/"+args[0], body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsArchivesDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a logs archive",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete logs archive %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting logs archive %s", args[0])); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/logs/config/archives/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Logs archive %s deleted\n", args[0])
		}
		return nil
	},
}

// rumlogsArchiveFields carries the flag values for a logs archive
// create/update request (create=POST, update=PUT full replacement).
type rumlogsArchiveFields struct {
	Name                    string
	Query                   string
	Destination             json.RawMessage
	CompressionMethod       string
	CompressionMethodSet    bool
	IncludeTags             bool
	IncludeTagsSet          bool
	RehydrationMaxScanGB    int64
	RehydrationMaxScanGBSet bool
	RehydrationTags         []string
}

func rumlogsArchiveBody(f rumlogsArchiveFields) map[string]any {
	attrs := map[string]any{
		"name":        f.Name,
		"query":       f.Query,
		"destination": f.Destination,
	}
	if f.CompressionMethodSet {
		attrs["compression_method"] = f.CompressionMethod
	}
	if f.IncludeTagsSet {
		attrs["include_tags"] = f.IncludeTags
	}
	if f.RehydrationMaxScanGBSet {
		attrs["rehydration_max_scan_size_in_gb"] = f.RehydrationMaxScanGB
	}
	if len(f.RehydrationTags) > 0 {
		attrs["rehydration_tags"] = f.RehydrationTags
	}

	return map[string]any{
		"data": map[string]any{
			"type":       "archives",
			"attributes": attrs,
		},
	}
}

// rumlogsParseDestinationJSON validates and wraps the raw --destination-json
// escape hatch used instead of modeling every s3/gcs/azure union field.
func rumlogsParseDestinationJSON(raw string) (json.RawMessage, error) {
	if raw == "" {
		return nil, fmt.Errorf("--destination-json is required")
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("--destination-json is not valid JSON: %s", raw)
	}
	return json.RawMessage(raw), nil
}

// ---------------------------------------------------------------------------
// logs custom destinations — api/v2/logs/config/custom-destinations
// ---------------------------------------------------------------------------

var rumlogsDestinationsCmd = &cobra.Command{
	Use:   "destinations",
	Short: "Manage logs custom destinations (api/v2/logs/config/custom-destinations)",
}

var rumlogsDestinationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List logs custom destinations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/custom-destinations", nil)
		if err != nil {
			return err
		}
		return printData("", flattenV2Items(extractData(data)))
	},
}

var rumlogsDestinationsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a logs custom destination by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/logs/config/custom-destinations/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsDestinationsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a logs custom destination",
	Long: `Create a logs custom destination. forwarder_destination (http/splunk/
elasticsearch/microsoft_sentinel union) is passed as raw JSON via
--forwarder-json rather than modeled flag-by-flag.

Examples:
  ddx logs destinations create --name "Nginx logs" --query "source:nginx" \
    --forwarder-json '{"type":"http","endpoint":"https://example.com","auth":{"type":"basic","username":"u","password":"p"}}'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create logs custom destination %q, no changes made\n", rumlogsDestName)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating logs custom destination %q", rumlogsDestName)); err != nil {
			return err
		}

		fwd, err := rumlogsParseForwarderJSON(rumlogsDestForwarderJSON)
		if err != nil {
			return err
		}
		f := rumlogsCustomDestFields{
			Name:                              rumlogsDestName,
			NameSet:                           true,
			Query:                             rumlogsDestQuery,
			QuerySet:                          cmd.Flags().Changed("query"),
			Enabled:                           rumlogsDestEnabled,
			EnabledSet:                        cmd.Flags().Changed("enabled"),
			ForwardTags:                       rumlogsDestForwardTags,
			ForwardTagsSet:                    cmd.Flags().Changed("forward-tags"),
			ForwardTagsRestrictionList:        rumlogsDestForwardTagsRestrictList,
			ForwardTagsRestrictionListType:    rumlogsDestForwardTagsRestrictType,
			ForwardTagsRestrictionListTypeSet: cmd.Flags().Changed("forward-tags-restriction-list-type"),
			ForwarderDestination:              fwd,
		}
		body := rumlogsCustomDestCreateBody(f)

		data, err := c.Post(context.Background(), "api/v2/logs/config/custom-destinations", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsDestinationsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a logs custom destination",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would update logs custom destination %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("updating logs custom destination %s", args[0])); err != nil {
			return err
		}

		var fwd json.RawMessage
		if rumlogsDestForwarderJSON != "" {
			var err error
			fwd, err = rumlogsParseForwarderJSON(rumlogsDestForwarderJSON)
			if err != nil {
				return err
			}
		}
		f := rumlogsCustomDestFields{
			Name:                              rumlogsDestName,
			NameSet:                           cmd.Flags().Changed("name"),
			Query:                             rumlogsDestQuery,
			QuerySet:                          cmd.Flags().Changed("query"),
			Enabled:                           rumlogsDestEnabled,
			EnabledSet:                        cmd.Flags().Changed("enabled"),
			ForwardTags:                       rumlogsDestForwardTags,
			ForwardTagsSet:                    cmd.Flags().Changed("forward-tags"),
			ForwardTagsRestrictionList:        rumlogsDestForwardTagsRestrictList,
			ForwardTagsRestrictionListType:    rumlogsDestForwardTagsRestrictType,
			ForwardTagsRestrictionListTypeSet: cmd.Flags().Changed("forward-tags-restriction-list-type"),
			ForwarderDestination:              fwd,
		}
		body := rumlogsCustomDestUpdateBody(args[0], f)

		data, err := c.Patch(context.Background(), "api/v2/logs/config/custom-destinations/"+args[0], body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var rumlogsDestinationsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a logs custom destination",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would delete logs custom destination %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("deleting logs custom destination %s", args[0])); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/logs/config/custom-destinations/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Logs custom destination %s deleted\n", args[0])
		}
		return nil
	},
}

// rumlogsCustomDestFields carries the flag values for a logs custom
// destination create/update request, plus whether each field was explicitly
// set (so update sends a true partial PATCH).
type rumlogsCustomDestFields struct {
	Name                              string
	NameSet                           bool
	Query                             string
	QuerySet                          bool
	Enabled                           bool
	EnabledSet                        bool
	ForwardTags                       bool
	ForwardTagsSet                    bool
	ForwardTagsRestrictionList        []string
	ForwardTagsRestrictionListType    string
	ForwardTagsRestrictionListTypeSet bool
	ForwarderDestination              json.RawMessage
}

func rumlogsCustomDestAttrs(f rumlogsCustomDestFields) map[string]any {
	attrs := map[string]any{}
	if f.NameSet {
		attrs["name"] = f.Name
	}
	if f.QuerySet {
		attrs["query"] = f.Query
	}
	if f.EnabledSet {
		attrs["enabled"] = f.Enabled
	}
	if f.ForwardTagsSet {
		attrs["forward_tags"] = f.ForwardTags
	}
	if len(f.ForwardTagsRestrictionList) > 0 {
		attrs["forward_tags_restriction_list"] = f.ForwardTagsRestrictionList
	}
	if f.ForwardTagsRestrictionListTypeSet {
		attrs["forward_tags_restriction_list_type"] = f.ForwardTagsRestrictionListType
	}
	if f.ForwarderDestination != nil {
		attrs["forwarder_destination"] = f.ForwarderDestination
	}
	return attrs
}

func rumlogsCustomDestCreateBody(f rumlogsCustomDestFields) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type":       "custom_destination",
			"attributes": rumlogsCustomDestAttrs(f),
		},
	}
}

func rumlogsCustomDestUpdateBody(id string, f rumlogsCustomDestFields) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":         id,
			"type":       "custom_destination",
			"attributes": rumlogsCustomDestAttrs(f),
		},
	}
}

// rumlogsParseForwarderJSON validates and wraps the raw --forwarder-json
// escape hatch used instead of modeling every http/splunk/elasticsearch/
// microsoft_sentinel union field.
func rumlogsParseForwarderJSON(raw string) (json.RawMessage, error) {
	if raw == "" {
		return nil, fmt.Errorf("--forwarder-json is required")
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("--forwarder-json is not valid JSON: %s", raw)
	}
	return json.RawMessage(raw), nil
}
