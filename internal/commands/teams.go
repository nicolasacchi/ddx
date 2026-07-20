package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

// New top-level command: Datadog Teams (api/v2/team). Supersedes the removed
// api/v2/on-call/teams (see on_call.go's now-deprecated `on-call teams` alias).
var (
	driftTeamsQuery              string
	driftTeamsMineOnly           bool
	driftTeamsIncludeMemberships bool
)

func init() {
	rootCmd.AddCommand(teamsCmd)
	teamsCmd.AddCommand(teamsListCmd)
	teamsCmd.AddCommand(teamsGetCmd)

	teamsListCmd.Flags().StringVar(&driftTeamsQuery, "query", "", "Search by team name, handle, or member email (filter[keyword])")
	teamsListCmd.Flags().BoolVar(&driftTeamsMineOnly, "mine", false, "Only show teams the current user belongs to (filter[me])")

	teamsGetCmd.Flags().BoolVar(&driftTeamsIncludeMemberships, "memberships", true, "Also fetch and merge team memberships into the output")
}

var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "Datadog Teams",
	Long: `List and inspect Datadog Teams (GET api/v2/team).

This is the canonical teams surface — api/v2/on-call/teams does not exist;
"ddx on-call teams" is kept only as a deprecated alias pointing here.`,
}

var teamsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List teams",
	Long: `List Datadog teams.

Examples:
  ddx teams list
  ddx teams list --query platform
  ddx teams list --mine --limit 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("page[size]", strconv.Itoa(limitFlag))
		if driftTeamsQuery != "" {
			params.Set("filter[keyword]", driftTeamsQuery)
		}
		if driftTeamsMineOnly {
			params.Set("filter[me]", "true")
		}

		data, err := c.Get(context.Background(), "api/v2/team", params)
		if err != nil {
			return err
		}
		return printData("teams.list", flattenV2Items(extractData(data)))
	},
}

var teamsGetCmd = &cobra.Command{
	Use:   "get <id-or-handle>",
	Short: "Get a team by UUID or handle",
	Long: `Get a single team (GET api/v2/team/{team_id}).

The API only supports lookup by UUID. If the argument doesn't look like a
UUID, it's treated as a team handle: teams are listed with
filter[keyword]=<arg> and matched by exact handle before the by-ID lookup.

By default also fetches api/v2/team/{team_id}/memberships and merges the
result under "memberships" in the output (--memberships=false to skip).

Examples:
  ddx teams get 00000000-0000-0000-0000-000000000001
  ddx teams get platform-team
  ddx teams get platform-team --memberships=false`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		teamID := args[0]
		if !uuidPattern.MatchString(teamID) {
			resolved, err := driftResolveTeamHandle(c, teamID)
			if err != nil {
				return err
			}
			teamID = resolved
		}

		data, err := c.Get(context.Background(), "api/v2/team/"+teamID, nil)
		if err != nil {
			return err
		}
		result := driftFlattenSingleV2Item(extractData(data))

		if driftTeamsIncludeMemberships {
			memberships, err := c.Get(context.Background(), "api/v2/team/"+teamID+"/memberships", nil)
			if err == nil {
				result = driftMergeTeamMemberships(result, memberships)
			}
			// Non-fatal: a memberships fetch failure (e.g. missing scope)
			// still leaves the team itself printed.
		}

		return printData("", result)
	},
}

// driftResolveTeamHandle looks up a team's UUID by handle, since GetTeam only
// accepts a team_id — there is no get-by-handle endpoint. It lists teams
// filtered by the handle as a search keyword and returns the first exact
// handle match.
func driftResolveTeamHandle(c *client.Client, handle string) (string, error) {
	params := url.Values{}
	params.Set("filter[keyword]", handle)
	params.Set("page[size]", "100")

	data, err := c.Get(context.Background(), "api/v2/team", params)
	if err != nil {
		return "", err
	}

	id, ok := driftFindTeamIDByHandle(extractData(data), handle)
	if !ok {
		return "", fmt.Errorf("no team found with handle %q", handle)
	}
	return id, nil
}

// driftFindTeamIDByHandle scans a ListTeams "data" array
// ([{"id","attributes":{"handle",...}}]) for an exact handle match and
// returns its id.
func driftFindTeamIDByHandle(data json.RawMessage, handle string) (string, bool) {
	var items []struct {
		ID         string `json:"id"`
		Attributes struct {
			Handle string `json:"handle"`
		} `json:"attributes"`
	}
	if json.Unmarshal(data, &items) != nil {
		return "", false
	}
	for _, item := range items {
		if item.Attributes.Handle == handle {
			return item.ID, true
		}
	}
	return "", false
}

// driftFlattenSingleV2Item merges a single v2 {"id","type","attributes":{...}}
// object's attributes and id, the same way flattenV2Items does per-element
// for an array. Falls back to the original bytes if the shape doesn't match.
func driftFlattenSingleV2Item(data json.RawMessage) json.RawMessage {
	wrapped := append(append([]byte("["), data...), ']')
	flat := flattenV2Items(wrapped)
	var arr []json.RawMessage
	if json.Unmarshal(flat, &arr) != nil || len(arr) != 1 {
		return data
	}
	return arr[0]
}

// driftMergeTeamMemberships merges a GetTeamMemberships response
// ({"data":[...]}) into an already-flattened team object under
// "memberships". team must already have its attributes merged (see
// driftFlattenSingleV2Item); membershipsRaw is the raw API response.
func driftMergeTeamMemberships(team json.RawMessage, membershipsRaw json.RawMessage) json.RawMessage {
	var teamObj map[string]any
	if json.Unmarshal(team, &teamObj) != nil {
		return team
	}

	var memberships any
	_ = json.Unmarshal(flattenV2Items(extractData(membershipsRaw)), &memberships)
	teamObj["memberships"] = memberships

	out, err := json.Marshal(teamObj)
	if err != nil {
		return team
	}
	return out
}
