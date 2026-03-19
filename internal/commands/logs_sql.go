package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nicolasacchi/ddx/internal/sqlparse"
	"github.com/spf13/cobra"
)

func init() {
	logsCmd.AddCommand(logsSQLCmd)
}

var logsSQLCmd = &cobra.Command{
	Use:   "sql <query>",
	Short: "Query logs with SQL syntax",
	Long: `Run SQL queries against logs. Translates SQL to the Datadog aggregate API.

Supported: SELECT with COUNT/AVG/SUM/MIN/MAX (multiple allowed), WHERE, GROUP BY,
           HAVING, ORDER BY, LIMIT, IN.
Not supported: JOIN, subqueries, DATE_TRUNC, window functions.

Examples:
  ddx logs sql "SELECT COUNT(*) FROM logs WHERE status = 'error'" --from 1h
  ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service LIMIT 10" --from 1h
  ddx logs sql "SELECT service, COUNT(*), AVG(@duration) FROM logs GROUP BY service" --from 1h
  ddx logs sql "SELECT service, COUNT(*) FROM logs GROUP BY service HAVING COUNT(*) > 100" --from 1h

WHERE translation:
  field = 'value'     →  field:value
  field > N           →  field:>N
  field IN ('a','b')  →  field:(a OR b)
  a = 'x' AND b = 'y' → a:x b:y`,
	Args: cobra.ExactArgs(1),
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

		// Parse SQL
		parsed, err := sqlparse.Parse(args[0])
		if err != nil {
			return fmt.Errorf("SQL parse error: %w", err)
		}

		// Extract all aggregates
		aggs := parsed.Aggregates()
		if len(aggs) == 0 {
			return fmt.Errorf("query must include an aggregate function (COUNT, AVG, SUM, MIN, MAX).\n" +
				"For raw log search, use: ddx logs search --query \"...\"")
		}

		// Build compute — support multiple aggregates
		var computes []map[string]any
		for _, agg := range aggs {
			computeObj := map[string]any{
				"aggregation": agg.Aggregate,
			}
			if agg.Metric != "" && agg.Metric != "*" {
				computeObj["metric"] = agg.Metric
			}
			computes = append(computes, computeObj)
		}

		// Build filter
		filter := parsed.Filter
		if filter == "" {
			filter = "*"
		}

		body := map[string]any{
			"filter": map[string]any{
				"query": filter,
				"from":  timeToISO(from),
				"to":    timeToISO(to),
			},
			"compute": computes,
		}

		// Build group_by from parsed GROUP BY (or plain SELECT fields)
		groupByFields := parsed.GroupBy
		if len(groupByFields) == 0 {
			groupByFields = parsed.PlainFields()
		}

		if len(groupByFields) > 0 {
			var groupBy []map[string]any
			for _, field := range groupByFields {
				gb := map[string]any{
					"facet": field,
					"sort":  map[string]any{"order": parsed.SortOrder},
				}
				if parsed.Limit > 0 {
					gb["limit"] = parsed.Limit
				}
				groupBy = append(groupBy, gb)
			}
			body["group_by"] = groupBy
		}

		data, err := c.Post(context.Background(), "api/v2/logs/analytics/aggregate", body)
		if err != nil {
			return err
		}

		// Apply HAVING filter client-side
		if parsed.HavingOp != "" {
			data = applyHaving(data, parsed.HavingOp, parsed.HavingValue)
		}

		// Show explorer URL
		if verboseFlag {
			explorerURL := buildExplorerURL("logs", filter, from, to)
			fmt.Fprintln(cmd.ErrOrStderr(), "Explorer:", explorerURL)
		}

		return printData("", data)
	},
}

// applyHaving filters aggregate buckets by a threshold on the first compute value.
func applyHaving(data json.RawMessage, op string, threshold float64) json.RawMessage {
	var resp struct {
		Data struct {
			Buckets []map[string]any `json:"buckets"`
		} `json:"data"`
		Meta json.RawMessage `json:"meta"`
	}
	if json.Unmarshal(data, &resp) != nil || len(resp.Data.Buckets) == 0 {
		return data
	}

	var filtered []map[string]any
	for _, bucket := range resp.Data.Buckets {
		computes, ok := bucket["computes"].(map[string]any)
		if !ok {
			continue
		}
		// Get first compute value (c0)
		val, ok := computes["c0"].(float64)
		if !ok {
			continue
		}
		if matchesHaving(val, op, threshold) {
			filtered = append(filtered, bucket)
		}
	}

	result := map[string]any{
		"data": map[string]any{"buckets": filtered},
		"meta": resp.Meta,
	}
	out, _ := json.Marshal(result)
	return out
}

func matchesHaving(val float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return val > threshold
	case ">=":
		return val >= threshold
	case "<":
		return val < threshold
	case "<=":
		return val <= threshold
	case "=":
		return val == threshold
	case "!=":
		return val != threshold
	}
	return true
}
