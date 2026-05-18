package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	incidentsQuery       string
	incidentsSort        string
	incidentTimeline     bool
	incidentTimelineFrom string
	incidentTimelineTo   string
	incidentsFacets      bool

	incidentState         string
	incidentSeverity      string
	incidentRootCause     string
	incidentRootCauseFile string
	incidentSummary       string
	incidentResolved      string
)

// incidentFieldTypes maps Datadog incident field names to their schema "type"
// values. Required because PATCH /api/v2/incidents/{uuid} expects each field
// to be a {type, value} object, and the type varies per field.
var incidentFieldTypes = map[string]string{
	"state":      "dropdown",
	"severity":   "dropdown",
	"root_cause": "textbox",
	"summary":    "textbox",
}

// uuidPattern matches RFC 4122 UUIDs (the form Datadog returns as data.id).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func init() {
	rootCmd.AddCommand(incidentsCmd)
	incidentsCmd.AddCommand(incidentsListCmd)
	incidentsCmd.AddCommand(incidentsGetCmd)
	incidentsCmd.AddCommand(incidentsFacetsCmd)
	incidentsCmd.AddCommand(incidentsUpdateCmd)
	incidentsCmd.AddCommand(incidentsResolveCmd)

	incidentsListCmd.Flags().StringVar(&incidentsQuery, "query", "state:active", "Search query (state, severity, team, commander, etc.)")
	incidentsListCmd.Flags().StringVar(&incidentsSort, "sort", "-created", "Sort field (created, -created, resolved, -severity, etc.)")

	incidentsGetCmd.Flags().BoolVar(&incidentTimeline, "timeline", false, "Include timeline with comments and status changes")
	incidentsGetCmd.Flags().StringVar(&incidentTimelineFrom, "timeline-from", "", "Filter timeline entries after this time")
	incidentsGetCmd.Flags().StringVar(&incidentTimelineTo, "timeline-to", "", "Filter timeline entries before this time")

	incidentsFacetsCmd.Flags().StringVar(&incidentsQuery, "query", "state:active", "Search query for facet aggregation")

	incidentsUpdateCmd.Flags().StringVar(&incidentState, "state", "", "New state: active, stable, resolved")
	incidentsUpdateCmd.Flags().StringVar(&incidentSeverity, "severity", "", "New severity: SEV-1, SEV-2, SEV-3, SEV-4, SEV-5")
	incidentsUpdateCmd.Flags().StringVar(&incidentRootCause, "root-cause", "", "Root-cause text")
	incidentsUpdateCmd.Flags().StringVar(&incidentRootCauseFile, "root-cause-file", "", "Read root cause from file (use '-' for stdin)")
	incidentsUpdateCmd.Flags().StringVar(&incidentSummary, "summary", "", "New summary text")
	incidentsUpdateCmd.Flags().StringVar(&incidentResolved, "resolved", "", "Resolved timestamp (RFC3339 or 'now'); auto-set to now when --state=resolved")

	incidentsResolveCmd.Flags().StringVar(&incidentRootCause, "root-cause", "", "Root-cause text to record on resolution")
	incidentsResolveCmd.Flags().StringVar(&incidentRootCauseFile, "root-cause-file", "", "Read root cause from file (use '-' for stdin)")
}

var incidentsCmd = &cobra.Command{
	Use:   "incidents",
	Short: "Search and inspect incidents",
}

var incidentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List incidents with rich query syntax",
	Long: `List incidents with faceted search.

Examples:
  ddx incidents list
  ddx incidents list --query "state:active severity:SEV-1"
  ddx incidents list --query "(state:active OR state:stable) AND team:backend"
  ddx incidents list --query "customer_impacted:true"
  ddx incidents list --query "commander.handle:user@example.com"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("query", incidentsQuery)
		params.Set("sort", incidentsSort)
		params.Set("page[size]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
		if err != nil {
			return err
		}

		incidents := extractIncidents(data)
		return printData("incidents.list", incidents)
	},
}

var incidentsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get incident details with optional timeline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if incidentTimeline {
			params.Set("include", "timeline")
		}

		data, err := c.Get(context.Background(), "api/v2/incidents/"+args[0], params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var incidentsFacetsCmd = &cobra.Command{
	Use:   "facets",
	Short: "Get faceted breakdown of incidents",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("query", incidentsQuery)
		params.Set("facets", "true")

		data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var incidentsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update fields on an incident",
	Long: `Update one or more fields on an incident. Accepts either the public id
(numeric, e.g. "45") or the UUID. Only flags you pass are sent — other fields
are left untouched.

Examples:
  ddx incidents update 45 --state stable
  ddx incidents update 45 --severity SEV-2 --summary "Re-classified after triage"
  ddx incidents update 45 --root-cause-file ./rca.md
  ddx incidents update 45 --state resolved --root-cause "Backfill job tuned"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		rootCause, err := readRootCause(incidentRootCause, incidentRootCauseFile)
		if err != nil {
			return err
		}

		fields := map[string]any{}
		addIncidentField(fields, "state", incidentState)
		addIncidentField(fields, "severity", incidentSeverity)
		addIncidentField(fields, "root_cause", rootCause)
		addIncidentField(fields, "summary", incidentSummary)

		resolved := incidentResolved
		if resolved == "" && incidentState == "resolved" {
			resolved = "now"
		}
		resolvedTS, err := normalizeResolved(resolved)
		if err != nil {
			return err
		}

		if len(fields) == 0 && resolvedTS == "" {
			return fmt.Errorf("no fields to update — pass at least one of --state, --severity, --root-cause[-file], --summary, --resolved")
		}

		return patchIncident(cmd, c, args[0], fields, resolvedTS)
	},
}

var incidentsResolveCmd = &cobra.Command{
	Use:   "resolve <id>",
	Short: "Resolve an incident (state=resolved, resolved=now)",
	Long: `Shortcut for "update <id> --state resolved --resolved now". Optionally
records a root cause via --root-cause or --root-cause-file.

Examples:
  ddx incidents resolve 45
  ddx incidents resolve 45 --root-cause "Self-healed after backfill drained"
  ddx incidents resolve 45 --root-cause-file ./rca.md`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		rootCause, err := readRootCause(incidentRootCause, incidentRootCauseFile)
		if err != nil {
			return err
		}

		fields := map[string]any{}
		addIncidentField(fields, "state", "resolved")
		addIncidentField(fields, "root_cause", rootCause)

		resolvedTS, _ := normalizeResolved("now")
		return patchIncident(cmd, c, args[0], fields, resolvedTS)
	},
}

// addIncidentField wraps a string value in the {type, value} envelope Datadog
// expects, only when the value is non-empty.
func addIncidentField(fields map[string]any, name, value string) {
	if value == "" {
		return
	}
	t, ok := incidentFieldTypes[name]
	if !ok {
		t = "textbox"
	}
	fields[name] = map[string]any{"type": t, "value": value}
}

// readRootCause resolves the root-cause string from the inline flag, a file,
// or stdin. Returns "" when neither flag is set. Errors if both are set.
func readRootCause(inline, path string) (string, error) {
	if inline != "" && path != "" {
		return "", fmt.Errorf("--root-cause and --root-cause-file are mutually exclusive")
	}
	if inline != "" {
		return inline, nil
	}
	if path == "" {
		return "", nil
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read root cause: %w", err)
	}
	return strings.TrimRight(string(data), " \t\r\n"), nil
}

// normalizeResolved converts "now" into an RFC3339 timestamp, leaves empty
// strings empty, and validates anything else parses as RFC3339.
func normalizeResolved(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value == "now" {
		return time.Now().UTC().Format(time.RFC3339), nil
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return "", fmt.Errorf("invalid --resolved %q: must be RFC3339 (e.g. 2026-05-18T09:30:00Z) or 'now'", value)
	}
	return value, nil
}

// resolveIncidentUUID returns the UUID for a numeric public id (via a GET
// round-trip) or echoes the input unchanged when it already looks like a UUID.
func resolveIncidentUUID(ctx context.Context, c *client.Client, id string) (string, error) {
	if uuidPattern.MatchString(id) {
		return id, nil
	}
	raw, err := c.Get(ctx, "api/v2/incidents/"+id, nil)
	if err != nil {
		return "", fmt.Errorf("resolve incident %s: %w", id, err)
	}
	var wrapper struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return "", fmt.Errorf("parse incident %s: %w", id, err)
	}
	if wrapper.Data.ID == "" {
		return "", fmt.Errorf("incident %s: missing data.id in response", id)
	}
	return wrapper.Data.ID, nil
}

// patchIncident builds and sends the PATCH body. fields is the {type,value}
// map already shaped for the incident schema; resolvedTS is set as the
// top-level "resolved" attribute when non-empty.
func patchIncident(cmd *cobra.Command, c *client.Client, id string, fields map[string]any, resolvedTS string) error {
	ctx := context.Background()
	uuid, err := resolveIncidentUUID(ctx, c, id)
	if err != nil {
		return err
	}

	attributes := map[string]any{}
	if len(fields) > 0 {
		attributes["fields"] = fields
	}
	if resolvedTS != "" {
		attributes["resolved"] = resolvedTS
	}

	body := map[string]any{
		"data": map[string]any{
			"id":         uuid,
			"type":       "incidents",
			"attributes": attributes,
		},
	}

	data, err := c.Patch(ctx, "api/v2/incidents/"+uuid, body)
	if err != nil {
		return err
	}
	return printData("", data)
}

func extractIncidents(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.Data != nil {
		// Flatten incidents from data[].attributes
		var items []json.RawMessage
		if json.Unmarshal(wrapper.Data, &items) == nil {
			var result []map[string]any
			for _, item := range items {
				var obj struct {
					ID         string          `json:"id"`
					Attributes json.RawMessage `json:"attributes"`
				}
				if json.Unmarshal(item, &obj) == nil {
					var attrs map[string]any
					if json.Unmarshal(obj.Attributes, &attrs) == nil {
						attrs["id"] = obj.ID
						result = append(result, attrs)
					}
				}
			}
			if result != nil {
				out, _ := json.Marshal(result)
				return out
			}
		}
		return wrapper.Data
	}
	return raw
}
