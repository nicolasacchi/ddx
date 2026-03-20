package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nicolasacchi/ddx/internal/sqlparse"
	"github.com/spf13/cobra"
)

var logsSQLExtraCols string

func init() {
	logsCmd.AddCommand(logsSQLCmd)
	logsSQLCmd.Flags().StringVar(&logsSQLExtraCols, "extra-columns", "", "JSON typed columns (e.g., '[{\"name\":\"@duration\",\"type\":\"float64\"}]')")
}

var logsSQLCmd = &cobra.Command{
	Use:   "sql <query>",
	Short: "Query logs with SQL syntax",
	Long: `Run SQL queries against logs. Translates SQL to the Datadog aggregate API.

Supported: SELECT with COUNT/AVG/SUM/MIN/MAX (multiple allowed), WHERE, GROUP BY,
           HAVING, ORDER BY, LIMIT, IN, DATE_TRUNC.
Not supported: JOIN, subqueries, window functions.

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
				// DATE_TRUNC_hour → timeseries with interval
				if strings.HasPrefix(field, "DATE_TRUNC_") {
					unit := strings.TrimPrefix(field, "DATE_TRUNC_")
					interval := dateTruncInterval(unit)
					// Add timeseries compute type
					for i := range computes {
						computes[i]["type"] = "timeseries"
						computes[i]["interval"] = interval
					}
					continue // Skip adding as group_by facet
				}
				gb := map[string]any{
					"facet": field,
					"sort":  map[string]any{"order": parsed.SortOrder},
				}
				if parsed.Limit > 0 {
					gb["limit"] = parsed.Limit
				}
				groupBy = append(groupBy, gb)
			}
			if len(groupBy) > 0 {
				body["group_by"] = groupBy
			}
		}

		// Extra typed columns
		if logsSQLExtraCols != "" {
			var cols []map[string]any
			if err := json.Unmarshal([]byte(logsSQLExtraCols), &cols); err != nil {
				return fmt.Errorf("invalid --extra-columns JSON: %w", err)
			}
			body["options"] = map[string]any{"extra_columns": cols}
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

func dateTruncInterval(unit string) int {
	switch unit {
	case "minute":
		return 60000
	case "hour":
		return 3600000
	case "day":
		return 86400000
	case "week":
		return 604800000
	default:
		return 3600000 // default to hour
	}
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
