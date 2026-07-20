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

	// --all pagination for incidents list.
	irIncidentsListAll bool

	// incidents create flags.
	irIncidentTitle       string
	irIncidentSeverity    string
	irIncidentSummary     string
	irIncidentImpacted    bool
	irIncidentImpactScope string
	irIncidentCommander   string
)

// irValidIncidentSeverities is the SEV-1..SEV-5 enum accepted by
// --severity on `incidents create` (and by extension `update`, though that
// path predates this package and isn't re-validated here).
var irValidIncidentSeverities = map[string]bool{
	"SEV-1": true, "SEV-2": true, "SEV-3": true, "SEV-4": true, "SEV-5": true,
}

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
	incidentsCmd.AddCommand(irIncidentsCreateCmd)

	incidentsListCmd.Flags().StringVar(&incidentsQuery, "query", "state:active", "Search query (state, severity, team, commander, etc.)")
	incidentsListCmd.Flags().StringVar(&incidentsSort, "sort", "-created", "Sort field (created, -created, resolved, -severity, etc.)")
	incidentsListCmd.Flags().BoolVar(&irIncidentsListAll, "all", false, "Fetch every result page (loops page[offset]/page[size] search pagination, capped at 20 pages) instead of just the first page")

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

	irIncidentsCreateCmd.Flags().StringVar(&irIncidentTitle, "title", "", "Incident title (required)")
	irIncidentsCreateCmd.Flags().StringVar(&irIncidentSeverity, "severity", "", "Severity: SEV-1, SEV-2, SEV-3, SEV-4, SEV-5")
	irIncidentsCreateCmd.Flags().StringVar(&irIncidentSummary, "summary", "", "Incident summary")
	irIncidentsCreateCmd.Flags().BoolVar(&irIncidentImpacted, "customer-impacted", false, "Flag the incident as customer-impacting")
	irIncidentsCreateCmd.Flags().StringVar(&irIncidentImpactScope, "customer-impact-scope", "", "Summary of customer impact; required when --customer-impacted is set")
	irIncidentsCreateCmd.Flags().StringVar(&irIncidentCommander, "commander", "", "Datadog user UUID to set as incident commander")
	irIncidentsCreateCmd.MarkFlagRequired("title")
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

		if irIncidentsListAll {
			const pageSize = 100 // page[size] max, per PageSize parameter in the spec
			fetch := func(offset, size int) ([]json.RawMessage, int, error) {
				params := url.Values{}
				params.Set("query", incidentsQuery)
				params.Set("sort", incidentsSort)
				params.Set("page[size]", strconv.Itoa(size))
				params.Set("page[offset]", strconv.Itoa(offset))
				data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
				if err != nil {
					return nil, 0, err
				}
				return irParseIncidentSearchPage(data)
			}
			items, _, err := paginateOffset(fetch, pageSize, 20)
			if err != nil {
				return err
			}
			return printData("incidents.list", irFlattenIncidentItems(items))
		}

		params := url.Values{}
		params.Set("query", incidentsQuery)
		params.Set("sort", incidentsSort)
		params.Set("page[size]", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v2/incidents/search", params)
		if err != nil {
			return err
		}

		items, total, err := irParseIncidentSearchPage(data)
		if err != nil {
			return err
		}
		if total > len(items) {
			fmt.Fprintf(os.Stderr, "incidents.list: showing %d of %d (use --all to fetch every page)\n", len(items), total)
		}
		return printData("incidents.list", irFlattenIncidentItems(items))
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
	// Gate before the UUID-resolution round-trip so --dry-run sends nothing.
	if dryRun() {
		fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would PATCH incident %s (fields=%v resolved=%q), no changes made\n", id, fields, resolvedTS)
		return nil
	}
	if err := requireConfirm(fmt.Sprintf("updating incident %s", id)); err != nil {
		return err
	}
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

// irParseIncidentSearchPage parses one page of a GET /api/v2/incidents/search
// response. Per the IncidentSearchResponse schema, results live at
// data.attributes.incidents (not the top-level data[] some other Datadog v2
// list endpoints use) and the reported total lives at data.attributes.total.
// Pure — no network — so it's directly unit-testable against fixture JSON.
func irParseIncidentSearchPage(raw json.RawMessage) ([]json.RawMessage, int, error) {
	var wrapper struct {
		Data struct {
			Attributes struct {
				Incidents []json.RawMessage `json:"incidents"`
				Total     int               `json:"total"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, 0, fmt.Errorf("parse incidents search response: %w", err)
	}
	return wrapper.Data.Attributes.Incidents, wrapper.Data.Attributes.Total, nil
}

// irUnwrapIncidentItem unwraps the per-incident envelope the search response
// schema documents (IncidentSearchResponseIncidentsData wraps each entry as
// {"data": {id, type, attributes}}, i.e. IncidentResponseData nested one
// level deeper than most v2 list endpoints). Falls back to the item itself
// when there's no nested "data" key, in case a given API version returns the
// flatter shape instead — cheap defensiveness against a spec/response
// mismatch we can't exercise against a live API in this environment.
func irUnwrapIncidentItem(item json.RawMessage) json.RawMessage {
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(item, &wrapper) == nil && wrapper.Data != nil {
		return wrapper.Data
	}
	return item
}

// irFlattenIncidentItems unwraps and flattens a page of incidents-search
// results into the same id+attributes shape flattenV2Items produces for
// other v2 list endpoints in this codebase.
func irFlattenIncidentItems(items []json.RawMessage) json.RawMessage {
	unwrapped := make([]json.RawMessage, len(items))
	for i, item := range items {
		unwrapped[i] = irUnwrapIncidentItem(item)
	}
	arr, err := json.Marshal(unwrapped)
	if err != nil {
		out, _ := json.Marshal(items)
		return out
	}
	return flattenV2Items(arr)
}

var irIncidentsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new incident",
	Long: `Create a new incident. Datadog assigns the incident number (public_id/slug)
on creation — this command never sets fields.slug or public_id itself (repo-wide
policy: IR- numbers belong to Datadog, never invented locally). The assigned
id is printed after creation.

Examples:
  ddx incidents create --title "Checkout errors spiking" --severity SEV-2 --yes
  ddx incidents create --title "Elevated 500s" --customer-impacted --customer-impact-scope "EU checkout" --yes
  ddx incidents create --title "Test drill" --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		body, err := irBuildIncidentCreateBody(irIncidentTitle, irIncidentSeverity, irIncidentSummary, irIncidentImpacted, irIncidentImpactScope, irIncidentCommander)
		if err != nil {
			return err
		}

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create incident %q (severity=%q customer_impacted=%v), no changes made\n", irIncidentTitle, irIncidentSeverity, irIncidentImpacted)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating incident %q", irIncidentTitle)); err != nil {
			return err
		}

		data, err := c.Post(context.Background(), "api/v2/incidents", body)
		if err != nil {
			return err
		}

		uuid, publicID, slug, title := irExtractIncidentIdentity(data)
		if !isJSONMode() {
			label := publicID
			if label == "" {
				label = slug
			}
			if label == "" {
				label = uuid
			}
			fmt.Fprintf(os.Stderr, "Created incident %s: %q (uuid=%s)\n", label, title, uuid)
		}

		return printData("", data)
	},
}

// irBuildIncidentCreateBody builds the POST /api/v2/incidents request body.
// Pure — validates the same required-field rules the spec documents
// (title, customer_impacted always; customer_impact_scope when impacted is
// true) and never emits fields.slug or public_id — Datadog assigns the
// incident number, so this function has no parameter that could set either.
func irBuildIncidentCreateBody(title, severity, summary string, impacted bool, impactScope, commander string) (map[string]any, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("--title is required")
	}
	if impacted && strings.TrimSpace(impactScope) == "" {
		return nil, fmt.Errorf("--customer-impact-scope is required when --customer-impacted is set")
	}
	if severity != "" && !irValidIncidentSeverities[severity] {
		return nil, fmt.Errorf("invalid --severity %q: must be one of SEV-1, SEV-2, SEV-3, SEV-4, SEV-5", severity)
	}

	fields := map[string]any{}
	addIncidentField(fields, "severity", severity)
	addIncidentField(fields, "summary", summary)

	attrs := map[string]any{
		"title":             title,
		"customer_impacted": impacted,
	}
	if impactScope != "" {
		attrs["customer_impact_scope"] = impactScope
	}
	if len(fields) > 0 {
		attrs["fields"] = fields
	}

	data := map[string]any{
		"type":       "incidents",
		"attributes": attrs,
	}
	if commander != "" {
		data["relationships"] = map[string]any{
			"commander_user": map[string]any{
				"data": map[string]any{
					"id":   commander,
					"type": "users",
				},
			},
		}
	}

	return map[string]any{"data": data}, nil
}

// irExtractIncidentIdentity pulls the identifiers worth printing prominently
// out of a CreateIncident/GetIncident response. public_id/slug aren't listed
// in the vendored IncidentResponseAttributes schema (it declares
// additionalProperties: {}, so undocumented fields are permitted) but are
// well-established as present on real incident objects — see CLAUDE.md Rule
// 8 ("slug == public_id") and resolveIncidentUUID's numeric-id round trip
// above. Returns empty strings for any field genuinely absent from the
// response rather than guessing.
func irExtractIncidentIdentity(raw json.RawMessage) (uuid, publicID, slug, title string) {
	var wrapper struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Title    string `json:"title"`
				PublicID string `json:"public_id"`
				Slug     string `json:"slug"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return "", "", "", ""
	}
	return wrapper.Data.ID, wrapper.Data.Attributes.PublicID, wrapper.Data.Attributes.Slug, wrapper.Data.Attributes.Title
}
