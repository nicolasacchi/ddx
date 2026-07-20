package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

// Live-probed 2026-07-20 (EU org): api/v2/security_monitoring/investigations -> 404.
// This is Bits AI investigations (x-unstable/preview), not Security Monitoring.
// Repointed to api/v2/bits-ai/investigations below.
const driftInvestigationsPageMax = 100 // page[limit] max per ListInvestigations

var driftInvestigationsMonitorID string

func init() {
	rootCmd.AddCommand(investigationsCmd)
	investigationsCmd.AddCommand(investigationsListCmd)
	investigationsCmd.AddCommand(investigationsGetCmd)

	investigationsListCmd.Flags().StringVar(&driftInvestigationsMonitorID, "monitor-id", "", "Filter investigations by monitor ID (filter[monitor_id])")
}

var investigationsCmd = &cobra.Command{
	Use:   "investigations",
	Short: "Bits AI investigations",
}

var investigationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Bits AI investigations",
	Long: `List Bits AI investigations for the organization (GET api/v2/bits-ai/investigations).
This is a preview/x-unstable Datadog API and may change.

--limit drives offset pagination (page[offset]/page[limit], max 100/page):
requesting more than 100 fetches additional pages until --limit is reached
or the API reports no more results.

Examples:
  ddx investigations list
  ddx investigations list --monitor-id 12345678 --limit 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		if driftInvestigationsMonitorID != "" {
			if _, err := strconv.ParseInt(driftInvestigationsMonitorID, 10, 64); err != nil {
				return fmt.Errorf("--monitor-id must be an integer, got %q", driftInvestigationsMonitorID)
			}
		}

		pageSize := limitFlag
		if pageSize <= 0 || pageSize > driftInvestigationsPageMax {
			pageSize = driftInvestigationsPageMax
		}
		maxPages := driftInvestigationsMaxPages(limitFlag, pageSize)

		items, total, err := paginateOffset(func(offset, size int) ([]json.RawMessage, int, error) {
			params := url.Values{}
			params.Set("page[offset]", strconv.Itoa(offset))
			params.Set("page[limit]", strconv.Itoa(size))
			if driftInvestigationsMonitorID != "" {
				params.Set("filter[monitor_id]", driftInvestigationsMonitorID)
			}
			data, err := c.Get(context.Background(), "api/v2/bits-ai/investigations", params)
			if err != nil {
				return nil, 0, err
			}
			return driftUnwrapInvestigationsPage(data)
		}, pageSize, maxPages)
		if err != nil {
			return err
		}

		if total > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "investigations: %d total\n", total)
		}
		if limitFlag > 0 && len(items) > limitFlag {
			items = items[:limitFlag]
		}
		if items == nil {
			items = []json.RawMessage{}
		}

		out, _ := json.Marshal(items)
		return printData("investigations.list", flattenV2Items(out))
	},
}

var investigationsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a Bits AI investigation by ID (includes conclusions)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/bits-ai/investigations/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

// driftInvestigationsMaxPages returns how many page[limit]=size pages are
// needed to satisfy a requested total of limit items. limit<=0 means "use
// the API/global default", i.e. a single page.
func driftInvestigationsMaxPages(limit, size int) int {
	if limit <= 0 || size <= 0 {
		return 1
	}
	n := (limit + size - 1) / size
	if n < 1 {
		n = 1
	}
	return n
}

// driftUnwrapInvestigationsPage unwraps one ListInvestigations page:
// {"data":[...], "meta":{"page":{"total":N}}}.
func driftUnwrapInvestigationsPage(raw json.RawMessage) ([]json.RawMessage, int, error) {
	var page struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Page struct {
				Total int `json:"total"`
			} `json:"page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, 0, err
	}
	return page.Data, page.Meta.Page.Total, nil
}
