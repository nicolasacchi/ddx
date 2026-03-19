package commands

import (
	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/nicolasacchi/ddx/internal/config"
	"github.com/nicolasacchi/ddx/internal/output"
	"github.com/nicolasacchi/ddx/internal/timeparse"
	"github.com/spf13/cobra"
)

var (
	version     = "dev"
	apiKeyFlag  string
	appKeyFlag  string
	siteFlag    string
	projectFlag string
	jsonFlag    bool
	jqFlag      string
	verboseFlag bool
	quietFlag   bool
	fromFlag    string
	toFlag      string
	limitFlag   int
)

var rootCmd = &cobra.Command{
	Use:   "ddx",
	Short: "ddx — Datadog Explorer CLI",
	Long: `ddx is a CLI for the Datadog API. Query logs, metrics, traces, monitors,
incidents, RUM events, and more — with SQL log analysis and multi-metric formulas.

Usage examples:
  ddx logs search --query "status:error" --from 1h
  ddx logs sql "SELECT service, count(*) FROM logs GROUP BY service" --from 1h
  ddx monitors list
  ddx metrics query --queries "avg:system.cpu.user{*}" --from 4h
  ddx incidents list --query "state:active"
  ddx error-tracking issues search --from 1d`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

func Execute() error {
	return rootCmd.Execute()
}

func getClient(cmd *cobra.Command) (*client.Client, error) {
	creds, err := config.LoadCredentials(apiKeyFlag, appKeyFlag, siteFlag, projectFlag)
	if err != nil {
		return nil, err
	}
	return client.New(creds.APIKey, creds.AppKey, creds.Site, verboseFlag), nil
}

func isJSONMode() bool {
	return output.IsJSON(jsonFlag, jqFlag)
}

func printData(command string, data []byte) error {
	return output.PrintData(command, data, isJSONMode(), jqFlag)
}

func parseFrom() (int64, error) {
	f := fromFlag
	if f == "" {
		f = "1h"
	}
	return timeparse.Parse(f)
}

func parseTo() (int64, error) {
	t := toFlag
	if t == "" {
		t = "now"
	}
	return timeparse.Parse(t)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiKeyFlag, "api-key", "", "Datadog API key (overrides DD_API_KEY)")
	rootCmd.PersistentFlags().StringVar(&appKeyFlag, "app-key", "", "Datadog App key (overrides DD_APP_KEY)")
	rootCmd.PersistentFlags().StringVar(&siteFlag, "site", "", "Datadog site (overrides DD_SITE, default: datadoghq.eu)")
	rootCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "Use a named project from config file")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Force JSON output (auto-enabled when stdout is not a TTY)")
	rootCmd.PersistentFlags().StringVar(&jqFlag, "jq", "", "Apply gjson path filter to JSON output")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Print request/response details to stderr")
	rootCmd.PersistentFlags().BoolVarP(&quietFlag, "quiet", "q", false, "Suppress non-error output")
	rootCmd.PersistentFlags().StringVar(&fromFlag, "from", "", "Time range start (e.g., 1h, 7d, now-2h, RFC3339)")
	rootCmd.PersistentFlags().StringVar(&toFlag, "to", "", "Time range end (default: now)")
	rootCmd.PersistentFlags().IntVar(&limitFlag, "limit", 50, "Max results to return")
}
