package commands

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(overviewCmd)
}

var overviewCmd = &cobra.Command{
	Use:   "overview",
	Short: "Parallel health snapshot across monitors, incidents, and errors",
	Long: `Fetch monitors in alert, active incidents, and top errors in parallel.

Examples:
  ddx overview
  ddx overview --from 1h`,
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

		result := map[string]any{}
		var mu sync.Mutex
		var wg sync.WaitGroup

		// Monitors in alert
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.Get(context.Background(), "api/v1/monitor", nil)
			if err != nil {
				mu.Lock()
				result["monitors_error"] = err.Error()
				mu.Unlock()
				return
			}
			alerting := filterByField(data, "overall_state", "Alert")
			warning := filterByField(data, "overall_state", "Warn")
			mu.Lock()
			result["monitors_alert"] = jsonLen(alerting)
			result["monitors_warn"] = jsonLen(warning)
			mu.Unlock()
		}()

		// Active incidents
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.Get(context.Background(), "api/v2/incidents/search?query=state:active", nil)
			if err != nil {
				mu.Lock()
				result["incidents_error"] = err.Error()
				mu.Unlock()
				return
			}
			var resp struct {
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(data, &resp) == nil {
				mu.Lock()
				result["active_incidents"] = jsonLen(resp.Data)
				mu.Unlock()
			}
		}()

		// Error tracking top issues
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := map[string]any{
				"data": map[string]any{
					"type": "search_request",
					"attributes": map[string]any{
						"query":   "*",
						"from":    from * 1000,
						"to":      to * 1000,
						"limit":   5,
						"sort":    "-error_count",
						"persona": "backend",
					},
				},
			}
			data, err := c.Post(context.Background(), "api/v2/error-tracking/issues/search", body)
			if err != nil {
				mu.Lock()
				result["errors_error"] = err.Error()
				mu.Unlock()
				return
			}
			issues := extractData(data)
			mu.Lock()
			result["top_errors"] = jsonLen(issues)
			mu.Unlock()
		}()

		// Log error count
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := map[string]any{
				"filter": map[string]any{
					"query": "status:error",
					"from":  timeToISO(from),
					"to":    timeToISO(to),
				},
				"compute": []map[string]any{
					{"aggregation": "count"},
				},
			}
			data, err := c.Post(context.Background(), "api/v2/logs/analytics/aggregate", body)
			if err != nil {
				mu.Lock()
				result["log_errors_error"] = err.Error()
				mu.Unlock()
				return
			}
			mu.Lock()
			var raw any
			json.Unmarshal(data, &raw)
			result["log_error_aggregate"] = raw
			mu.Unlock()
		}()

		wg.Wait()

		out, _ := json.Marshal(result)
		return printData("", out)
	},
}

func jsonLen(data json.RawMessage) int {
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) == nil {
		return len(arr)
	}
	return 0
}
