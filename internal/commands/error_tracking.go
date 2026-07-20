package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

var (
	etQuery string
	etTrack string

	irETStateFlag    string
	irETAssigneeFlag string
)

func init() {
	rootCmd.AddCommand(errorTrackingCmd)
	errorTrackingCmd.AddCommand(etIssuesCmd)
	etIssuesCmd.AddCommand(etIssuesSearchCmd)
	etIssuesCmd.AddCommand(etIssuesGetCmd)
	etIssuesCmd.AddCommand(irETSetStateCmd)
	etIssuesCmd.AddCommand(irETAssignCmd)

	etIssuesSearchCmd.Flags().StringVar(&etQuery, "query", "", "Filter query (e.g., service:1000farmacie)")
	etIssuesSearchCmd.Flags().StringVar(&etTrack, "persona", "backend", "Error source: backend, frontend, mobile, or all (fans out and merges all three)")

	irETSetStateCmd.Flags().StringVar(&irETStateFlag, "state", "", "New state: OPEN, ACKNOWLEDGED, RESOLVED, IGNORED, EXCLUDED (required)")
	irETSetStateCmd.MarkFlagRequired("state")

	irETAssignCmd.Flags().StringVar(&irETAssigneeFlag, "assignee", "", "Datadog user UUID, or email to resolve via GET /api/v2/users (required)")
	irETAssignCmd.MarkFlagRequired("assignee")
}

var errorTrackingCmd = &cobra.Command{
	Use:   "error-tracking",
	Short: "Search and inspect error tracking issues",
}

var etIssuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "Manage error tracking issues",
}

// irPersonaFanout is the fixed fan-out order for --persona all. Fixing the
// order makes the merge deterministic regardless of which goroutine happens
// to finish first, and gives ties in irMergePersonaIssues's sort a stable
// tiebreak.
var irPersonaFanout = []string{"backend", "frontend", "mobile"}

// irPersonaAPIValue maps ddx's user-facing --persona value to the Datadog API's
// IssuesSearchRequestDataAttributesPersona enum (ALL, BROWSER, MOBILE, BACKEND).
// Note the API's own name for the frontend/browser track is "BROWSER", not
// "FRONTEND" — ddx keeps the friendlier "frontend" on the CLI surface and
// translates it here.
func irPersonaAPIValue(persona string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(persona)) {
	case "backend":
		return "BACKEND", nil
	case "frontend":
		return "BROWSER", nil
	case "mobile":
		return "MOBILE", nil
	case "all":
		return "ALL", nil
	default:
		return "", fmt.Errorf("invalid --persona %q: must be one of backend, frontend, mobile, all", persona)
	}
}

// irBuildIssueSearchBody builds the POST /api/v2/error-tracking/issues/search
// request body. Pure — no network. Only carries the fields the
// IssuesSearchRequestDataAttributes schema actually documents (query, from,
// to, persona); earlier code here also sent "sort" and "page.limit", neither
// of which exists in that schema — dropped as dead weight rather than kept
// for a compatibility that was never real.
func irBuildIssueSearchBody(query string, from, to int64, personaAPIValue string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"type": "search_request",
			"attributes": map[string]any{
				"query":   query,
				"from":    from * 1000,
				"to":      to * 1000,
				"persona": personaAPIValue,
			},
		},
	}
}

// irParseIssueItems unwraps a search response's data[] into flattened
// id+attributes maps, silently (no stderr total-count line) — used by the
// --persona all fan-out where three concurrent "N total results" lines from
// extractWithMeta would just be noise; the merged/truncated total is
// reported once at the call site instead.
func irParseIssueItems(raw json.RawMessage) ([]map[string]any, error) {
	flattened := flattenV2Items(extractData(raw))
	var items []map[string]any
	if err := json.Unmarshal(flattened, &items); err != nil {
		return nil, fmt.Errorf("parse issue search response: %w", err)
	}
	return items, nil
}

// irMergePersonaIssues merges per-persona issue lists (each already
// flattened to id+attributes maps by irParseIssueItems), stamping a
// "persona" field on every item, and returns them sorted by total_count
// descending. total_count is the closest analogue in the actual response
// schema to the "-error_count" ordering the single-persona path has always
// requested via a "sort" parameter that isn't part of the documented
// request schema (see irBuildIssueSearchBody) — so this keeps the same
// intent (highest-impact issues first) without relying on a request field
// that was never real. Ties keep irPersonaFanout order, then original
// per-persona API order (sort.SliceStable).
func irMergePersonaIssues(perPersona map[string][]map[string]any) []map[string]any {
	var merged []map[string]any
	for _, persona := range irPersonaFanout {
		for _, item := range perPersona[persona] {
			item["persona"] = persona
			merged = append(merged, item)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return irIssueTotalCount(merged[i]) > irIssueTotalCount(merged[j])
	})
	return merged
}

func irIssueTotalCount(item map[string]any) float64 {
	if v, ok := item["total_count"].(float64); ok {
		return v
	}
	return 0
}

// irFetchPersonaIssues is the network-calling closure type consumed by
// irSearchIssuesAllPersonas; production code passes a closure hitting the
// real API, tests pass a fake — same shape as paginateOffset's fetch func.
type irFetchPersonaIssues func(persona string) ([]map[string]any, error)

// irSearchIssuesAllPersonas fans out one search per persona in
// irPersonaFanout concurrently (the same goroutine+mutex pattern overview.go
// uses for its parallel health snapshot), merges and tags the results, and
// returns partial data with stderr warnings when only some personas failed —
// only returning an error when every persona failed.
func irSearchIssuesAllPersonas(fetch irFetchPersonaIssues) ([]map[string]any, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[string][]map[string]any{}
	errs := map[string]error{}

	for _, persona := range irPersonaFanout {
		wg.Add(1)
		go func(persona string) {
			defer wg.Done()
			items, err := fetch(persona)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[persona] = err
				return
			}
			results[persona] = items
		}(persona)
	}
	wg.Wait()

	if len(errs) == len(irPersonaFanout) {
		for persona, err := range errs {
			return nil, fmt.Errorf("all persona searches failed (e.g. %s): %w", persona, err)
		}
	}
	for _, persona := range irPersonaFanout {
		if err, ok := errs[persona]; ok {
			fmt.Fprintf(os.Stderr, "error-tracking.search: %s persona failed: %v\n", persona, err)
		}
	}
	return irMergePersonaIssues(results), nil
}

// irTruncateMarker reports whether n items should be truncated down to
// limit, and how many to keep. limit<=0 means "no limit" (printData's
// separate agent-mode row cap handles that case).
func irTruncateMarker(n, limit int) (truncated bool, keep int) {
	if limit <= 0 || n <= limit {
		return false, n
	}
	return true, limit
}

var etIssuesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search error tracking issues",
	Long: `Search error tracking issues.

Examples:
  ddx error-tracking issues search --from 1d
  ddx error-tracking issues search --query "service:1000farmacie" --from 7d
  ddx error-tracking issues search --from 1d --limit 10
  ddx error-tracking issues search --persona all --from 1d`,
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

		q := etQuery
		if q == "" {
			q = "*"
		}

		var items []map[string]any

		if strings.EqualFold(strings.TrimSpace(etTrack), "all") {
			items, err = irSearchIssuesAllPersonas(func(persona string) ([]map[string]any, error) {
				apiValue, mapErr := irPersonaAPIValue(persona)
				if mapErr != nil {
					return nil, mapErr
				}
				body := irBuildIssueSearchBody(q, from, to, apiValue)
				data, postErr := c.Post(context.Background(), "api/v2/error-tracking/issues/search", body)
				if postErr != nil {
					return nil, postErr
				}
				return irParseIssueItems(data)
			})
			if err != nil {
				return err
			}
		} else {
			apiValue, mapErr := irPersonaAPIValue(etTrack)
			if mapErr != nil {
				return mapErr
			}
			body := irBuildIssueSearchBody(q, from, to, apiValue)
			data, postErr := c.Post(context.Background(), "api/v2/error-tracking/issues/search", body)
			if postErr != nil {
				return postErr
			}
			issues := extractWithMeta(data, "error-tracking")
			flattened := flattenV2Items(issues)
			if err := json.Unmarshal(flattened, &items); err != nil {
				return fmt.Errorf("parse issue search response: %w", err)
			}
		}

		total := len(items)
		truncated, keep := irTruncateMarker(total, limitFlag)
		if truncated {
			fmt.Fprintf(os.Stderr, "error-tracking.search: showing %d of %d (raise --limit to see more)\n", keep, total)
		}
		items = items[:keep]

		out, err := json.Marshal(items)
		if err != nil {
			return fmt.Errorf("marshal issue search results: %w", err)
		}

		return printData("error-tracking.search", out)
	},
}

var etIssuesGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Get error tracking issue details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/error-tracking/issues/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

// irValidIssueStates is the IssueState enum (PUT .../state).
var irValidIssueStates = map[string]bool{
	"OPEN": true, "ACKNOWLEDGED": true, "RESOLVED": true, "IGNORED": true, "EXCLUDED": true,
}

// irNormalizeIssueState upper-cases and validates --state against the
// IssueState enum. Pure.
func irNormalizeIssueState(s string) (string, error) {
	up := strings.ToUpper(strings.TrimSpace(s))
	if !irValidIssueStates[up] {
		return "", fmt.Errorf("invalid --state %q: must be one of OPEN, ACKNOWLEDGED, RESOLVED, IGNORED, EXCLUDED", s)
	}
	return up, nil
}

// irBuildIssueStateBody builds the PUT .../state request body. Pure.
func irBuildIssueStateBody(issueID, state string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   issueID,
			"type": "error_tracking_issue",
			"attributes": map[string]any{
				"state": state,
			},
		},
	}
}

// irBuildIssueAssigneeBody builds the PUT .../assignee request body. Pure.
func irBuildIssueAssigneeBody(assigneeID string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":   assigneeID,
			"type": "assignee",
		},
	}
}

// irIsEmail is a deliberately simple email-vs-uuid discriminator for
// --assignee: any "@" means "look this up", anything else is passed through
// as a literal user id. Pure.
func irIsEmail(s string) bool {
	return strings.Contains(s, "@")
}

// irResolveAssigneeID resolves --assignee to a Datadog user UUID. UUID-shaped
// (or at least non-email) input passes through unchanged; email input is
// resolved via GET /api/v2/users?filter=<email> (ListUsers' free-text filter
// param). This intentionally does its own resolution here rather than
// depending on anything in users.go, which this package doesn't own.
func irResolveAssigneeID(ctx context.Context, c *client.Client, assignee string) (string, error) {
	if !irIsEmail(assignee) {
		return assignee, nil
	}
	params := url.Values{}
	params.Set("filter", assignee)
	data, err := c.Get(ctx, "api/v2/users", params)
	if err != nil {
		return "", fmt.Errorf("resolve assignee %s: %w", assignee, err)
	}
	var wrapper struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return "", fmt.Errorf("parse users response for %s: %w", assignee, err)
	}
	if len(wrapper.Data) == 0 {
		return "", fmt.Errorf("no Datadog user found matching %q", assignee)
	}
	return wrapper.Data[0].ID, nil
}

var irETSetStateCmd = &cobra.Command{
	Use:   "set-state <issue-id>",
	Short: "Update the state of an error tracking issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := irNormalizeIssueState(irETStateFlag)
		if err != nil {
			return err
		}

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would set issue %s state to %s, no changes made\n", args[0], state)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("setting issue %s state to %s", args[0], state)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		body := irBuildIssueStateBody(args[0], state)
		data, err := c.Put(context.Background(), "api/v2/error-tracking/issues/"+args[0]+"/state", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var irETAssignCmd = &cobra.Command{
	Use:   "assign <issue-id>",
	Short: "Assign an error tracking issue to a user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would assign issue %s to %s, no changes made\n", args[0], irETAssigneeFlag)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("assigning issue %s to %s", args[0], irETAssigneeFlag)); err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		assigneeID, err := irResolveAssigneeID(context.Background(), c, irETAssigneeFlag)
		if err != nil {
			return err
		}

		body := irBuildIssueAssigneeBody(assigneeID)
		data, err := c.Put(context.Background(), "api/v2/error-tracking/issues/"+args[0]+"/assignee", body)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
