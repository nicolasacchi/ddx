package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

// Live-probed 2026-07-20 (EU org): api/v2/scorecards -> 404, api/v2/scorecards/rules -> 404.
// Both dead wrappers repointed below to their real ListScorecards/ListScorecardRules paths.
var (
	driftScorecardsID          string
	driftScorecardsName        string
	driftScorecardsDescription string

	driftRulesID               string
	driftRulesName             string
	driftRulesDescription      string
	driftRulesEnabledOnly      bool
	driftRulesCustomOnly       bool
	driftRulesIncludeScorecard bool
)

func init() {
	rootCmd.AddCommand(scorecardsCmd)
	scorecardsCmd.AddCommand(scorecardsListCmd)
	scorecardsCmd.AddCommand(scorecardsRulesCmd)

	scorecardsListCmd.Flags().StringVar(&driftScorecardsID, "id", "", "Filter by scorecard ID")
	scorecardsListCmd.Flags().StringVar(&driftScorecardsName, "name", "", "Filter by scorecard name (partial match)")
	scorecardsListCmd.Flags().StringVar(&driftScorecardsDescription, "description", "", "Filter by scorecard description (partial match)")

	scorecardsRulesCmd.Flags().StringVar(&driftRulesID, "rule-id", "", "Filter by rule UUID")
	scorecardsRulesCmd.Flags().StringVar(&driftRulesName, "name", "", "Filter by rule name")
	scorecardsRulesCmd.Flags().StringVar(&driftRulesDescription, "description", "", "Filter by rule description")
	scorecardsRulesCmd.Flags().BoolVar(&driftRulesEnabledOnly, "enabled-only", false, "Only show enabled rules")
	scorecardsRulesCmd.Flags().BoolVar(&driftRulesCustomOnly, "custom-only", false, "Only show custom rules")
	scorecardsRulesCmd.Flags().BoolVar(&driftRulesIncludeScorecard, "include-scorecard", true, "Merge each rule's parent scorecard (name/description) into its output")
}

var scorecardsCmd = &cobra.Command{
	Use:   "scorecards",
	Short: "Service scorecards and rules",
}

var scorecardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scorecards",
	Long: `List service scorecards (GET api/v2/scorecard/scorecards).

Examples:
  ddx scorecards list
  ddx scorecards list --name Observability --limit 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("page[size]", strconv.Itoa(limitFlag))
		if driftScorecardsID != "" {
			params.Set("filter[scorecard][id]", driftScorecardsID)
		}
		if driftScorecardsName != "" {
			params.Set("filter[scorecard][name]", driftScorecardsName)
		}
		if driftScorecardsDescription != "" {
			params.Set("filter[scorecard][description]", driftScorecardsDescription)
		}

		data, err := c.Get(context.Background(), "api/v2/scorecard/scorecards", params)
		if err != nil {
			return err
		}
		return printData("scorecards.list", flattenV2Items(extractData(data)))
	},
}

var scorecardsRulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "List scorecard rules",
	Long: `List scorecard rules (GET api/v2/scorecard/rules), optionally merging each
rule's parent scorecard (via ?include=scorecard) into the output.

Examples:
  ddx scorecards rules
  ddx scorecards rules --enabled-only --custom-only
  ddx scorecards rules --name "Code Repos Defined"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("page[size]", strconv.Itoa(limitFlag))
		if driftRulesIncludeScorecard {
			params.Set("include", "scorecard")
		}
		if driftRulesID != "" {
			params.Set("filter[rule][id]", driftRulesID)
		}
		if driftRulesName != "" {
			params.Set("filter[rule][name]", driftRulesName)
		}
		if driftRulesDescription != "" {
			params.Set("filter[rule][description]", driftRulesDescription)
		}
		if driftRulesEnabledOnly {
			params.Set("filter[rule][enabled]", "true")
		}
		if driftRulesCustomOnly {
			params.Set("filter[rule][custom]", "true")
		}

		data, err := c.Get(context.Background(), "api/v2/scorecard/rules", params)
		if err != nil {
			return err
		}
		return printData("scorecards.rules", driftMergeRuleScorecards(data))
	},
}

// driftRuleItem/driftIncludedScorecard mirror the shapes documented for
// ListScorecardRules: {"data":[{"id","attributes","relationships":{"scorecard":{"data":{"id"}}}}],"included":[{"id","attributes"}]}.
type driftRuleItem struct {
	ID            string          `json:"id"`
	Attributes    json.RawMessage `json:"attributes"`
	Relationships struct {
		Scorecard struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"scorecard"`
	} `json:"relationships"`
}

type driftIncludedScorecard struct {
	ID         string          `json:"id"`
	Attributes json.RawMessage `json:"attributes"`
}

// driftMergeRuleScorecards flattens a ListScorecardRules response into a
// single array of rule attribute objects (id merged in, same as
// flattenV2Items), each carrying its parent scorecard's attributes under
// "scorecard" when the response included one (i.e. ?include=scorecard was
// set and the rule has a scorecard relationship). Falls back to plain
// extractData if the response isn't in the expected v2 shape.
func driftMergeRuleScorecards(raw json.RawMessage) json.RawMessage {
	var wrapper struct {
		Data     []driftRuleItem          `json:"data"`
		Included []driftIncludedScorecard `json:"included"`
	}
	if json.Unmarshal(raw, &wrapper) != nil || wrapper.Data == nil {
		return flattenV2Items(extractData(raw))
	}

	scorecardsByID := make(map[string]json.RawMessage, len(wrapper.Included))
	for _, inc := range wrapper.Included {
		scorecardsByID[inc.ID] = inc.Attributes
	}

	result := make([]map[string]any, 0, len(wrapper.Data))
	for _, rule := range wrapper.Data {
		var attrs map[string]any
		if json.Unmarshal(rule.Attributes, &attrs) != nil || attrs == nil {
			attrs = map[string]any{}
		}
		attrs["id"] = rule.ID
		if scID := rule.Relationships.Scorecard.Data.ID; scID != "" {
			if scAttrs, ok := scorecardsByID[scID]; ok {
				var sc any
				if json.Unmarshal(scAttrs, &sc) == nil {
					attrs["scorecard"] = sc
				}
			}
		}
		result = append(result, attrs)
	}

	out, _ := json.Marshal(result)
	return out
}
