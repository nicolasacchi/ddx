package commands

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

// Live-probed 2026-07-20 (EU org): api/v2/network/flows -> 404 (no such path in the
// spec at all). Replaced with the two real Cloud Network Monitoring aggregate
// endpoints, which probed 400 with only ?limit=1 — from/to/group_by are filled
// in below from global --from/--to plus a --group-by flag so the request is
// well-formed.
var (
	driftNetConnGroupBy string
	driftNetConnTags    string
	driftNetConnQuery   string
	driftNetConnLimit   int

	driftNetDNSGroupBy string
	driftNetDNSTags    string
	driftNetDNSQuery   string
	driftNetDNSLimit   int
)

func init() {
	rootCmd.AddCommand(networkCmd)
	networkCmd.AddCommand(networkDevicesCmd)
	networkCmd.AddCommand(networkConnectionsCmd)
	networkCmd.AddCommand(networkDNSCmd)

	networkConnectionsCmd.Flags().StringVar(&driftNetConnGroupBy, "group-by", "client_service,server_service", "Comma-separated fields to group connections by (max 10)")
	networkConnectionsCmd.Flags().StringVar(&driftNetConnTags, "tags", "", "Comma-separated tags to filter by (ignored if --query is set)")
	networkConnectionsCmd.Flags().StringVar(&driftNetConnQuery, "query", "", `Free-form query, e.g. "(client_team:networks OR client_team:platform) AND server_service:x" — takes precedence over --tags`)
	networkConnectionsCmd.Flags().IntVar(&driftNetConnLimit, "network-limit", 100, "Max connections to return (server-side cap, max 7500)")

	networkDNSCmd.Flags().StringVar(&driftNetDNSGroupBy, "group-by", "network.dns_query", "Comma-separated fields to group DNS traffic by (max 10)")
	networkDNSCmd.Flags().StringVar(&driftNetDNSTags, "tags", "", "Comma-separated tags to filter by (ignored if --query is set)")
	networkDNSCmd.Flags().StringVar(&driftNetDNSQuery, "query", "", `Free-form query, e.g. "(client_team:networks OR client_team:platform) AND server_service:x" — takes precedence over --tags`)
	networkDNSCmd.Flags().IntVar(&driftNetDNSLimit, "network-limit", 100, "Max aggregated DNS entries to return (server-side cap, max 7500)")
}

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Network devices, connections, and DNS traffic",
}

var networkDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List network devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/ndm/devices", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var networkConnectionsCmd = &cobra.Command{
	Use:   "connections",
	Short: "Aggregated network connections (Cloud Network Monitoring)",
	Long: `Get aggregated network connections (GET api/v2/network/connections/aggregate).

Requires Cloud Network Monitoring (CNM) to be enabled on the org. When CNM
isn't enabled, Datadog returns a bare 400 "Bad Request" here (not 403) even
for an otherwise spec-valid request — live-probed 2026-07-20 (EU org).

Examples:
  ddx network connections --from 1h
  ddx network connections --group-by client_service,server_service --from 4h
  ddx network connections --query "server_service:checkout" --from 1h`,
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

		params := driftBuildNetworkAggregateParams(from, to, driftNetConnGroupBy, driftNetConnTags, driftNetConnQuery, driftNetConnLimit)
		data, err := c.Get(context.Background(), "api/v2/network/connections/aggregate", params)
		if err != nil {
			return driftAnnotateCNMHint(err)
		}
		return printData("network.connections", flattenV2Items(extractData(data)))
	},
}

var networkDNSCmd = &cobra.Command{
	Use:   "dns",
	Short: "Aggregated DNS traffic (Cloud Network Monitoring)",
	Long: `Get aggregated DNS traffic (GET api/v2/network/dns/aggregate).

Requires Cloud Network Monitoring (CNM) to be enabled on the org. When CNM
isn't enabled, Datadog returns a bare 400 "Bad Request" here (not 403) even
for an otherwise spec-valid request — live-probed 2026-07-20 (EU org).

Examples:
  ddx network dns --from 1h
  ddx network dns --group-by network.dns_query --from 4h
  ddx network dns --query "client_service:checkout" --from 1h`,
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

		params := driftBuildNetworkAggregateParams(from, to, driftNetDNSGroupBy, driftNetDNSTags, driftNetDNSQuery, driftNetDNSLimit)
		data, err := c.Get(context.Background(), "api/v2/network/dns/aggregate", params)
		if err != nil {
			return driftAnnotateCNMHint(err)
		}
		return printData("network.dns", flattenV2Items(extractData(data)))
	},
}

// driftBuildNetworkAggregateParams builds the query params shared by
// GetAggregatedConnections and GetAggregatedDns: from/to (unix seconds),
// group_by, and either query (free-form, takes precedence per spec) or tags,
// plus an optional server-side limit.
func driftBuildNetworkAggregateParams(from, to int64, groupBy, tags, query string, limit int) url.Values {
	params := url.Values{}
	params.Set("from", strconv.FormatInt(from, 10))
	params.Set("to", strconv.FormatInt(to, 10))
	if groupBy != "" {
		params.Set("group_by", groupBy)
	}
	if query != "" {
		params.Set("query", query)
	} else if tags != "" {
		params.Set("tags", tags)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	return params
}

// driftAnnotateCNMHint sets a Hint on a bare "400: Bad Request" response from
// the network aggregate endpoints. Datadog returns exactly this — no further
// detail, and 400 rather than 403 — when Cloud Network Monitoring isn't
// enabled on the org, even for an otherwise spec-valid request (live-probed
// 2026-07-20, EU org, via bare curl). Leaves any other error untouched.
func driftAnnotateCNMHint(err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 && strings.EqualFold(strings.TrimSpace(apiErr.Detail), "Bad Request") {
		apiErr.Hint = "Cloud Network Monitoring (CNM) may not be enabled on this org — Datadog returns a bare 400 here when it isn't"
	}
	return err
}
