package commands

import (
	"context"
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

Supported: SELECT with COUNT/AVG/SUM/MIN/MAX, WHERE, GROUP BY, ORDER BY, LIMIT.
Not supported: JOIN, HAVING, subqueries, DATE_TRUNC, window functions.

Examples:
  ddx logs sql "SELECT COUNT(*) FROM logs WHERE status = 'error'" --from 1h
  ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service LIMIT 10" --from 1h
  ddx logs sql "SELECT @http.status_code, AVG(@duration) FROM logs GROUP BY @http.status_code" --from 4h
  ddx logs sql "SELECT service, COUNT(*) FROM logs WHERE service IN ('web', 'api') GROUP BY service" --from 1h

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

		// Extract aggregate
		agg := parsed.HasAggregate()
		if agg == nil {
			return fmt.Errorf("query must include an aggregate function (COUNT, AVG, SUM, MIN, MAX).\n" +
				"For raw log search, use: ddx logs search --query \"...\"")
		}

		// Build compute
		computeObj := map[string]any{
			"aggregation": agg.Aggregate,
		}
		if agg.Metric != "" && agg.Metric != "*" {
			computeObj["metric"] = agg.Metric
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
			"compute": []map[string]any{computeObj},
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

		return printData("", data)
	},
}
