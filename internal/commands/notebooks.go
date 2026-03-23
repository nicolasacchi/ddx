package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	notebooksQuery  string
	notebooksSort   string
	notebooksFilter string
	nbName          string
	nbCells         string
	nbType          string
	nbTimeSpan      string
	nbAppend        bool
)

func init() {
	rootCmd.AddCommand(notebooksCmd)
	notebooksCmd.AddCommand(notebooksListCmd)
	notebooksCmd.AddCommand(notebooksGetCmd)
	notebooksCmd.AddCommand(notebooksSearchCmd)
	notebooksCmd.AddCommand(notebooksCreateCmd)
	notebooksCmd.AddCommand(notebooksEditCmd)
	notebooksCmd.AddCommand(notebooksDeleteCmd)

	notebooksSearchCmd.Flags().StringVar(&notebooksQuery, "query", "", "Search by name or content")
	notebooksSearchCmd.Flags().StringVar(&notebooksSort, "sort", "-modified_at", "Sort field")

	notebooksCreateCmd.Flags().StringVar(&nbName, "name", "", "Notebook name (required)")
	notebooksCreateCmd.Flags().StringVar(&nbCells, "cells", "", "JSON cells array or @filename")
	notebooksCreateCmd.Flags().StringVar(&nbType, "type", "investigation", "Type: investigation, postmortem, runbook, documentation, report")
	notebooksCreateCmd.Flags().StringVar(&nbTimeSpan, "time-span", "1h", "Time span: 1h, 4h, 1d, 1w, etc.")
	notebooksCreateCmd.MarkFlagRequired("name")
	notebooksCreateCmd.MarkFlagRequired("cells")

	notebooksEditCmd.Flags().StringVar(&nbCells, "cells", "", "JSON cells array (required)")
	notebooksEditCmd.Flags().BoolVar(&nbAppend, "append", false, "Append cells instead of replacing")
	notebooksEditCmd.MarkFlagRequired("cells")
}

var notebooksCmd = &cobra.Command{
	Use:   "notebooks",
	Short: "Manage Datadog notebooks",
}

var notebooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List notebooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("count", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v1/notebooks", params)
		if err != nil {
			return err
		}

		return printData("", extractWithMeta(data, "notebooks"))
	},
}

var notebooksGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get notebook details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v1/notebooks/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}

var notebooksSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search notebooks by name or content",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		if notebooksQuery != "" {
			params.Set("query", notebooksQuery)
		}
		params.Set("sort_field", notebooksSort)
		params.Set("count", strconv.Itoa(limitFlag))

		data, err := c.Get(context.Background(), "api/v1/notebooks", params)
		if err != nil {
			return err
		}

		return printData("", extractWithMeta(data, "notebooks"))
	},
}

var notebooksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a notebook with typed cells",
	Long: `Create a notebook with markdown, metric, and log cells.

Cell JSON format:
  [{"type":"markdown","data":"# Title"},{"type":"metric","data":"avg:system.cpu.user{*}","title":"CPU"}]

Examples:
  ddx notebooks create --name "Investigation" --cells '[{"type":"markdown","data":"# Summary"}]'
  ddx notebooks create --name "Report" --cells @cells.json --type report`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		// Parse cells
		var cells []map[string]any
		cellsJSON := nbCells
		if len(cellsJSON) > 0 && cellsJSON[0] == '@' {
			// TODO: read from file
			return fmt.Errorf("file references not yet supported, pass JSON directly")
		}
		if err := json.Unmarshal([]byte(cellsJSON), &cells); err != nil {
			return fmt.Errorf("invalid --cells JSON: %w", err)
		}

		// Convert simplified cells to Datadog notebook cell format
		var nbCells []map[string]any
		for _, cell := range cells {
			cellType, _ := cell["type"].(string)
			switch cellType {
			case "markdown":
				nbCells = append(nbCells, map[string]any{
					"attributes": map[string]any{
						"definition": map[string]any{
							"type": "markdown",
							"text": cell["data"],
						},
					},
					"type": "notebook_cells",
				})
			case "metric":
				nbCells = append(nbCells, map[string]any{
					"attributes": map[string]any{
						"definition": map[string]any{
							"type": "timeseries",
							"requests": []map[string]any{
								{
									"q":            cell["data"],
									"display_type": "line",
								},
							},
						},
						"graph_size": "m",
					},
					"type": "notebook_cells",
				})
			case "logs":
				nbCells = append(nbCells, map[string]any{
					"attributes": map[string]any{
						"definition": map[string]any{
							"type": "timeseries",
							"requests": []map[string]any{
								{
									"q":           cell["data"],
									"data_source": "logs",
								},
							},
						},
					},
					"type": "notebook_cells",
				})
			}
		}

		body := map[string]any{
			"data": map[string]any{
				"type": "notebooks",
				"attributes": map[string]any{
					"name":   nbName,
					"status": "published",
					"time": map[string]any{
						"live_span": nbTimeSpan,
					},
					"cells":    nbCells,
					"metadata": map[string]any{"type": nbType},
				},
			},
		}

		data, err := c.Post(context.Background(), "api/v1/notebooks", body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var notebooksEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a notebook's cells",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		var cells json.RawMessage
		if err := json.Unmarshal([]byte(nbCells), &cells); err != nil {
			return fmt.Errorf("invalid --cells JSON: %w", err)
		}

		if nbAppend {
			// Fetch existing notebook first
			existing, err := c.Get(context.Background(), "api/v1/notebooks/"+args[0], nil)
			if err != nil {
				return fmt.Errorf("fetch existing notebook: %w", err)
			}

			var nb struct {
				Data struct {
					Attributes struct {
						Cells json.RawMessage `json:"cells"`
					} `json:"attributes"`
				} `json:"data"`
			}
			if err := json.Unmarshal(existing, &nb); err != nil {
				return fmt.Errorf("parse notebook: %w", err)
			}

			// Merge: existing + new
			var existingCells, newCells []json.RawMessage
			json.Unmarshal(nb.Data.Attributes.Cells, &existingCells)
			json.Unmarshal(cells, &newCells)
			merged := append(existingCells, newCells...)
			cells, _ = json.Marshal(merged)
		}

		body := map[string]any{
			"data": map[string]any{
				"type": "notebooks",
				"attributes": map[string]any{
					"cells": json.RawMessage(cells),
				},
			},
		}

		data, err := c.Put(context.Background(), "api/v1/notebooks/"+args[0], body)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var notebooksDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a notebook",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Delete(context.Background(), "api/v1/notebooks/"+args[0]); err != nil {
			return err
		}
		if !quietFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Notebook %s deleted\n", args[0])
		}
		return nil
	},
}
