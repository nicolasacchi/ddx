package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nicolasacchi/ddx/internal/timeparse"
	"github.com/spf13/cobra"
)

var (
	irDowntimeScope       string
	irDowntimeMonitorID   int64
	irDowntimeMonitorTags string
	irDowntimeStart       string
	irDowntimeEnd         string
	irDowntimeMessage     string
)

func init() {
	rootCmd.AddCommand(downtimesCmd)
	downtimesCmd.AddCommand(downtimesListCmd)
	downtimesCmd.AddCommand(downtimesGetCmd)
	downtimesCmd.AddCommand(downtimesCancelCmd)
	downtimesCmd.AddCommand(irDowntimesCreateCmd)

	irDowntimesCreateCmd.Flags().StringVar(&irDowntimeScope, "scope", "", "Scope the downtime applies to, e.g. \"env:(staging OR prod) AND datacenter:us-east-1\" (required)")
	irDowntimesCreateCmd.Flags().Int64Var(&irDowntimeMonitorID, "monitor-id", 0, "Mute only this monitor id (mutually exclusive with --monitor-tags)")
	irDowntimesCreateCmd.Flags().StringVar(&irDowntimeMonitorTags, "monitor-tags", "", "Comma-separated monitor tags to mute (mutually exclusive with --monitor-id); omit both to mute all monitors in scope")
	irDowntimesCreateCmd.Flags().StringVar(&irDowntimeStart, "start", "", "Downtime start: RFC3339, now, now-1h, epoch, or a bare duration like 2h — bare durations are future-anchored (2h = two hours from now, not two hours ago); omitted = starts immediately")
	irDowntimesCreateCmd.Flags().StringVar(&irDowntimeEnd, "end", "", "Downtime end (same forms as --start; bare durations are future-anchored, e.g. --end 2h = two hours from now); omitted = never ends")
	irDowntimesCreateCmd.Flags().StringVar(&irDowntimeMessage, "message", "", "Message to include with downtime notifications")
	irDowntimesCreateCmd.MarkFlagRequired("scope")
}

var downtimesCmd = &cobra.Command{
	Use:   "downtimes",
	Short: "Manage maintenance downtimes",
}

var downtimesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List downtimes",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/downtime", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var downtimesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get downtime details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/downtime/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var downtimesCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a downtime",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would cancel downtime %s, no changes made\n", args[0])
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("cancelling downtime %s", args[0])); err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v2/downtime/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Downtime %s cancelled\n", args[0])
		}
		return nil
	},
}

var irDowntimesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Schedule a maintenance downtime",
	Long: `Schedule a one-time maintenance downtime. Only one-time schedules are
supported (no recurrence modeling) — pass --start/--end for a fixed window, or
omit both for a downtime that starts immediately and never ends.

--start/--end accept RFC3339, "now", "now-1h"/"now+1h", epoch seconds, or a
bare duration like "2h" — bare durations are future-anchored (2h = two hours
from now), matching how a maintenance window is normally described ("starts
now, lasts 2h"), not timeparse's usual now-X lookback convention. When both
resolve, --end must be strictly after --start.

Examples:
  ddx downtimes create --scope "env:prod" --monitor-tags "service:checkout" --message "Deploy window" --yes
  ddx downtimes create --scope "env:staging" --monitor-id 123 --start now --end 2h --yes
  ddx downtimes create --scope "env:prod" --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		startTS, err := irFormatDowntimeBoundary(irDowntimeStart)
		if err != nil {
			return fmt.Errorf("--start: %w", err)
		}
		endTS, err := irFormatDowntimeBoundary(irDowntimeEnd)
		if err != nil {
			return fmt.Errorf("--end: %w", err)
		}
		if err := irValidateDowntimeWindow(startTS, endTS); err != nil {
			return err
		}

		body, err := irBuildDowntimeCreateBody(irDowntimeScope, cmd.Flags().Changed("monitor-id"), irDowntimeMonitorID, irDowntimeMonitorTags, startTS, endTS, irDowntimeMessage)
		if err != nil {
			return err
		}

		if dryRun() {
			fmt.Fprintf(cmd.OutOrStdout(), "--dry-run: would create downtime (scope=%q), no changes made\n", irDowntimeScope)
			return nil
		}
		if err := requireConfirm(fmt.Sprintf("creating downtime (scope=%q)", irDowntimeScope)); err != nil {
			return err
		}

		data, err := c.Post(context.Background(), "api/v2/downtime", body)
		if err != nil {
			return err
		}

		if !isJSONMode() {
			var wrapper struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(data, &wrapper) == nil && wrapper.Data.ID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Created downtime %s\n", wrapper.Data.ID)
			}
		}

		return printData("", data)
	},
}

// irFormatDowntimeBoundary converts a timeparse-form flag value (empty,
// relative, RFC3339, or Unix) into the RFC3339 string the downtime schedule
// attributes expect. Empty input passes through unchanged — omitting a
// schedule boundary is meaningful (start defaults to "now", end defaults to
// "never").
//
// timeparse.Parse anchors bare durations ("2h") in the past (now-2h) — it's
// built for --from/--to lookback windows. A maintenance window described as
// "--start now --end 2h" means the opposite: end should be two hours in the
// FUTURE, not before start. So bare durations get a dedicated future-anchored
// (now+duration) interpretation here; every other form ("now", RFC3339, epoch,
// explicit now-X/now+X) passes straight through to timeparse.Parse unchanged.
func irFormatDowntimeBoundary(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if irIsBareDuration(value) {
		dur, err := timeparse.ParseDuration(value)
		if err != nil {
			return "", err
		}
		return time.Now().Add(dur).UTC().Format(time.RFC3339), nil
	}
	sec, err := timeparse.Parse(value)
	if err != nil {
		return "", err
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339), nil
}

// irIsBareDuration reports whether value is a plain duration (e.g. "2h",
// "30m", "1.5d") with no "now"/"now-"/"now+" prefix and that isn't RFC3339 or
// a Unix timestamp — i.e. exactly the form timeparse.Parse's "pure relative"
// branch treats as now-duration (a past anchor), which downtimes create
// instead future-anchors. "now", "now-X", "now+X", RFC3339, and epoch forms
// are excluded here so they keep going through timeparse.Parse unchanged.
func irIsBareDuration(value string) bool {
	if value == "" || value == "now" {
		return false
	}
	if strings.HasPrefix(value, "now-") || strings.HasPrefix(value, "now+") {
		return false
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return false
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return false
	}
	_, err := timeparse.ParseDuration(value)
	return err == nil
}

// irValidateDowntimeWindow rejects a downtime schedule whose end boundary
// does not strictly follow its start boundary. startTS/endTS are the
// pre-formatted RFC3339 strings irFormatDowntimeBoundary returns (or "" when
// that boundary was omitted) — validation only applies when both are
// present, since a single open-ended boundary has no ordering to violate.
// Called before dryRun()/requireConfirm() so even a --dry-run preview catches
// an inverted window.
func irValidateDowntimeWindow(startTS, endTS string) error {
	if startTS == "" || endTS == "" {
		return nil
	}
	startT, err := time.Parse(time.RFC3339, startTS)
	if err != nil {
		return fmt.Errorf("parse --start: %w", err)
	}
	endT, err := time.Parse(time.RFC3339, endTS)
	if err != nil {
		return fmt.Errorf("parse --end: %w", err)
	}
	if !endT.After(startT) {
		return fmt.Errorf("--end (%s) must be after --start (%s)", endTS, startTS)
	}
	return nil
}

// irBuildDowntimeCreateBody builds the POST /api/v2/downtime request body.
// Pure — no network — so it's directly unit-testable. monitorIDSet must
// reflect cmd.Flags().Changed("monitor-id") since 0 is a valid-looking (if
// implausible) monitor id and can't be used as an "unset" sentinel on its
// own. --monitor-id and non-empty --monitor-tags are mutually exclusive;
// DowntimeMonitorIdentifier is required by the spec, so when neither is
// given this defaults to monitor_tags=["*"] (mute all monitors in scope),
// matching the spec's own documented use of that value for exactly this
// purpose. startTS/endTS are pre-formatted RFC3339 strings (or "" to omit).
// Recurrence is intentionally not modeled — only the one-time schedule shape.
func irBuildDowntimeCreateBody(scope string, monitorIDSet bool, monitorID int64, monitorTagsRaw, startTS, endTS, message string) (map[string]any, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, fmt.Errorf("--scope is required")
	}
	if monitorIDSet && monitorTagsRaw != "" {
		return nil, fmt.Errorf("--monitor-id and --monitor-tags are mutually exclusive")
	}

	attrs := map[string]any{
		"scope": scope,
	}

	switch {
	case monitorIDSet:
		attrs["monitor_identifier"] = map[string]any{"monitor_id": monitorID}
	case monitorTagsRaw != "":
		tags := splitComma(monitorTagsRaw)
		if len(tags) == 0 {
			return nil, fmt.Errorf("--monitor-tags must contain at least one tag")
		}
		attrs["monitor_identifier"] = map[string]any{"monitor_tags": tags}
	default:
		attrs["monitor_identifier"] = map[string]any{"monitor_tags": []string{"*"}}
	}

	if message != "" {
		attrs["message"] = message
	}

	if startTS != "" || endTS != "" {
		schedule := map[string]any{}
		if startTS != "" {
			schedule["start"] = startTS
		}
		if endTS != "" {
			schedule["end"] = endTS
		}
		attrs["schedule"] = schedule
	}

	return map[string]any{
		"data": map[string]any{
			"type":       "downtime",
			"attributes": attrs,
		},
	}, nil
}
