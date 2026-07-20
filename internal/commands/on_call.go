package commands

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var onCallTeam string

// Live-probed 2026-07-20 (EU org): api/v2/on-call/teams -> 404 (no such path in the
// spec — on-call has per-team on-call/routing-rules sub-resources, but no top-level
// team list). Repointed the `on-call teams` alias to GET api/v2/team below; the new
// top-level `ddx teams` command (teams.go) is the non-deprecated home for this.
var (
	driftOnCallPageTeamID     string
	driftOnCallPageTeamHandle string
	driftOnCallPageUserID     string
	driftOnCallPageTitle      string
	driftOnCallPageDesc       string
	driftOnCallPageUrgency    string
	driftOnCallPageTags       string

	driftOnCallRespSchedule string
	driftOnCallRespInclude  string
	driftOnCallRespPosition string
	driftOnCallRespAtTS     string
)

func init() {
	rootCmd.AddCommand(onCallCmd)
	onCallCmd.AddCommand(onCallTeamsCmd)
	onCallCmd.AddCommand(onCallSchedulesCmd)
	onCallCmd.AddCommand(onCallPageCmd)
	onCallCmd.AddCommand(onCallRespondersCmd)

	onCallSchedulesCmd.Flags().StringVar(&onCallTeam, "team", "", "Filter by team")

	onCallPageCmd.Flags().StringVar(&driftOnCallPageTeamID, "team-id", "", "Target team UUID (exactly one of --team-id/--team-handle/--user-id is required)")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageTeamHandle, "team-handle", "", "Target team handle (exactly one of --team-id/--team-handle/--user-id is required)")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageUserID, "user-id", "", "Target user UUID (exactly one of --team-id/--team-handle/--user-id is required)")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageTitle, "title", "", "Page title (required)")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageDesc, "description", "", "Short summary of the issue or context")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageUrgency, "urgency", "high", "Urgency: low or high")
	onCallPageCmd.Flags().StringVar(&driftOnCallPageTags, "tags", "", "Comma-separated tags, e.g. service:checkout")
	onCallPageCmd.MarkFlagRequired("title")

	onCallRespondersCmd.Flags().StringVar(&driftOnCallRespSchedule, "schedule", "", "Schedule UUID (required)")
	onCallRespondersCmd.Flags().StringVar(&driftOnCallRespInclude, "include", "responders,responders.shifts,responders.shifts.user", "Comma-separated included relationships")
	onCallRespondersCmd.Flags().StringVar(&driftOnCallRespPosition, "position", "", "Comma-separated positions: previous,current,next (default: current)")
	onCallRespondersCmd.Flags().StringVar(&driftOnCallRespAtTS, "at", "", "RFC3339 timestamp to query responders at (default: now)")
	onCallRespondersCmd.MarkFlagRequired("schedule")
}

var onCallCmd = &cobra.Command{
	Use:   "on-call",
	Short: "On-call teams, schedules, pages, and responders",
}

var onCallTeamsCmd = &cobra.Command{
	Use:        "teams",
	Short:      "List teams (deprecated alias — use `ddx teams list`)",
	Deprecated: "api/v2/on-call/teams no longer exists; this now calls api/v2/team — use `ddx teams list` instead",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		params.Set("page[size]", strconv.Itoa(limitFlag))
		data, err := c.Get(context.Background(), "api/v2/team", params)
		if err != nil {
			return err
		}
		return printData("on-call.teams", flattenV2Items(extractData(data)))
	},
}

var onCallSchedulesCmd = &cobra.Command{
	Use:   "schedules",
	Short: "List on-call schedules",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		if onCallTeam != "" {
			params.Set("filter[team]", onCallTeam)
		}
		data, err := c.Get(context.Background(), "api/v2/on-call/schedules", params)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var onCallPageCmd = &cobra.Command{
	Use:   "page",
	Short: "Trigger an on-call page",
	Long: `Trigger a new On-Call page against a team or user (POST api/v2/on-call/pages).

Examples:
  ddx on-call page --team-handle my-team --title "DB replica lag" --urgency high --yes
  ddx on-call page --user-id 00000000-0000-0000-0000-000000000001 --title "Manual escalation" --urgency low --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		body, err := driftBuildOnCallPageBody(driftOnCallPageTeamID, driftOnCallPageTeamHandle, driftOnCallPageUserID, driftOnCallPageTitle, driftOnCallPageDesc, driftOnCallPageUrgency, driftOnCallPageTags)
		if err != nil {
			return err
		}

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create on-call page %q (urgency=%s), no data sent\n", driftOnCallPageTitle, driftOnCallPageUrgency)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("triggering on-call page %q", driftOnCallPageTitle)); err != nil {
			return err
		}

		data, err := c.Post(context.Background(), "api/v2/on-call/pages", body)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var onCallRespondersCmd = &cobra.Command{
	Use:   "responders",
	Short: "Get on-call responders for a schedule",
	Long: `Get the previous/current/next on-call responders for a schedule
(GET api/v2/on-call/schedules/{schedule_id}/responders).

Examples:
  ddx on-call responders --schedule 3653d3c6-0c75-11ea-ad28-fb5701eabc7d
  ddx on-call responders --schedule <id> --position previous,current,next
  ddx on-call responders --schedule <id> --at 2026-07-20T10:00:00Z`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if driftOnCallRespInclude != "" {
			params.Set("include", driftOnCallRespInclude)
		}
		if driftOnCallRespPosition != "" {
			params.Set("filter[position]", driftOnCallRespPosition)
		}
		if driftOnCallRespAtTS != "" {
			params.Set("filter[at_ts]", driftOnCallRespAtTS)
		}

		data, err := c.Get(context.Background(), "api/v2/on-call/schedules/"+driftOnCallRespSchedule+"/responders", params)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

// driftBuildOnCallPageBody validates flag inputs and builds the
// CreateOnCallPage request body ({"data":{"type":"pages","attributes":{...}}}).
// Exactly one of teamID/teamHandle/userID must be set (they map to the
// target.type enum team_id|team_handle|user_id); title is required; urgency
// must be "low" or "high".
func driftBuildOnCallPageBody(teamID, teamHandle, userID, title, description, urgency, tagsCSV string) (map[string]any, error) {
	var identifier, targetType string
	targets := 0
	if teamID != "" {
		targets++
		identifier, targetType = teamID, "team_id"
	}
	if teamHandle != "" {
		targets++
		identifier, targetType = teamHandle, "team_handle"
	}
	if userID != "" {
		targets++
		identifier, targetType = userID, "user_id"
	}
	if targets == 0 {
		return nil, fmt.Errorf("one of --team-id, --team-handle, or --user-id is required")
	}
	if targets > 1 {
		return nil, fmt.Errorf("only one of --team-id, --team-handle, or --user-id may be set")
	}
	if title == "" {
		return nil, fmt.Errorf("--title is required")
	}
	if urgency != "low" && urgency != "high" {
		return nil, fmt.Errorf("--urgency must be \"low\" or \"high\", got %q", urgency)
	}

	attrs := map[string]any{
		"target": map[string]any{
			"identifier": identifier,
			"type":       targetType,
		},
		"title":   title,
		"urgency": urgency,
	}
	if description != "" {
		attrs["description"] = description
	}
	if tags := splitComma(tagsCSV); len(tags) > 0 {
		attrs["tags"] = tags
	}

	return map[string]any{
		"data": map[string]any{
			"type":       "pages",
			"attributes": attrs,
		},
	}, nil
}
