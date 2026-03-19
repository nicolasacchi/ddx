package commands

import (
	"encoding/json"
	"fmt"

	"github.com/nicolasacchi/ddx/internal/config"
	"github.com/spf13/cobra"
)

var (
	configAPIKey string
	configAppKey string
	configSite   string
)

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configAddCmd)
	configCmd.AddCommand(configRemoveCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configCurrentCmd)

	configAddCmd.Flags().StringVar(&configAPIKey, "api-key", "", "Datadog API key (required)")
	configAddCmd.Flags().StringVar(&configAppKey, "app-key", "", "Datadog App key (required)")
	configAddCmd.Flags().StringVar(&configSite, "site", "datadoghq.eu", "Datadog site")
	configAddCmd.MarkFlagRequired("api-key")
	configAddCmd.MarkFlagRequired("app-key")
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage ddx configuration",
}

var configAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a project configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.AddProject(args[0], configAPIKey, configAppKey, configSite); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Project %q added\n", args[0])
		return nil
	},
}

var configRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a project configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.RemoveProject(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Project %q removed\n", args[0])
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ListProjects()
		if err != nil {
			return fmt.Errorf("no config file found — run 'ddx config add' to create one")
		}
		if cfg.Projects == nil || len(cfg.Projects) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No projects configured")
			return nil
		}

		var rows []map[string]any
		for name, p := range cfg.Projects {
			rows = append(rows, map[string]any{
				"name":    name,
				"api_key": config.MaskKey(p.APIKey),
				"app_key": config.MaskKey(p.AppKey),
				"site":    p.Site,
				"default": name == cfg.DefaultProject,
			})
		}

		data, _ := json.Marshal(rows)
		return printData("config.list", data)
	},
}

var configUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the default project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.SetDefaultProject(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Default project set to %q\n", args[0])
		return nil
	},
}

var configCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show current project configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ListProjects()
		if err != nil {
			return fmt.Errorf("no config file found")
		}
		if cfg.DefaultProject == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "No default project set")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Current project: %s\n", cfg.DefaultProject)
		return nil
	},
}
