package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

var (
	servicesQuery    string
	servicesDetailed bool
	depsDirection    string
	depsMermaid      bool
)

func init() {
	rootCmd.AddCommand(servicesCmd)
	servicesCmd.AddCommand(servicesListCmd)
	servicesCmd.AddCommand(servicesGetCmd)
	servicesCmd.AddCommand(servicesDepsCmd)
	servicesCmd.AddCommand(servicesTeamCmd)

	servicesListCmd.Flags().StringVar(&servicesQuery, "query", "", "Filter by name or team (e.g., name:web* AND team:backend)")
	servicesListCmd.Flags().BoolVar(&servicesDetailed, "detailed", false, "Include external resource links")

	servicesDepsCmd.Flags().StringVar(&depsDirection, "direction", "downstream", "Dependency direction: upstream or downstream")
	servicesDepsCmd.Flags().BoolVar(&depsMermaid, "mermaid", false, "Output as Mermaid diagram")
}

var servicesCmd = &cobra.Command{
	Use:   "services",
	Short: "List services, dependencies, and team ownership",
}

var servicesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List APM services",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		// Auto-paginate to get all services
		var allItems []json.RawMessage
		pageSize := 100
		page := 0
		for {
			params := url.Values{}
			if servicesQuery != "" {
				params.Set("filter", servicesQuery)
			}
			params.Set("page[size]", fmt.Sprintf("%d", pageSize))
			params.Set("page[number]", fmt.Sprintf("%d", page))

			data, err := c.Get(context.Background(), "api/v2/services/definitions", params)
			if err != nil {
				return err
			}

			items := extractData(data)
			var batch []json.RawMessage
			if json.Unmarshal(items, &batch) != nil || len(batch) == 0 {
				break
			}
			allItems = append(allItems, batch...)

			if len(batch) < pageSize {
				break
			}
			page++
			if page > 20 { // safety limit
				break
			}
		}

		// Also fetch APM-discovered services from dependency map and merge
		now := time.Now().Unix()
		dayAgo := now - 86400
		depsParams := url.Values{}
		depsParams.Set("env", "production")
		depsParams.Set("start", fmt.Sprintf("%d", dayAgo))
		depsParams.Set("end", fmt.Sprintf("%d", now))
		apmData, err := c.Get(context.Background(), "api/v1/service_dependencies", depsParams)
		if err == nil {
			var apmServices map[string]any
			if json.Unmarshal(apmData, &apmServices) == nil {
				// Build set of catalog service names
				catalogNames := map[string]bool{}
				for _, item := range allItems {
					var obj map[string]any
					if json.Unmarshal(item, &obj) == nil {
						if attrs, ok := obj["attributes"].(map[string]any); ok {
							if schema, ok := attrs["schema"].(map[string]any); ok {
								if name, ok := schema["dd-service"].(string); ok {
									catalogNames[name] = true
								}
							}
						}
					}
				}
				// Add APM services not in catalog
				for name := range apmServices {
					if !catalogNames[name] {
						entry, _ := json.Marshal(map[string]any{
							"type": "apm-service",
							"id":   name,
							"attributes": map[string]any{
								"schema": map[string]any{"dd-service": name},
								"meta":   map[string]any{"source": "apm"},
							},
						})
						allItems = append(allItems, entry)
					}
				}
			}
		}

		if allItems == nil {
			allItems = []json.RawMessage{}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "services: %d total\n", len(allItems))
		out, _ := json.Marshal(allItems)
		return printData("", out)
	},
}

var servicesGetCmd = &cobra.Command{
	Use:   "get <service-name>",
	Short: "Get service details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("schema_version", "v2.2")

		data, err := c.Get(context.Background(), "api/v2/services/definitions/"+args[0], params)
		if err != nil {
			return err
		}

		return printData("", data)
	},
}

var servicesDepsCmd = &cobra.Command{
	Use:   "deps <service-name>",
	Short: "Show service dependencies (upstream/downstream)",
	Long: `Show upstream or downstream dependencies for a service.

Examples:
  ddx services deps web-1000farmacie --direction downstream
  ddx services deps web-1000farmacie --direction upstream --mermaid`,
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

		params := url.Values{}
		params.Set("env", "production")
		params.Set("start", fmt.Sprintf("%d", from))
		params.Set("end", fmt.Sprintf("%d", to))

		data, err := c.Get(context.Background(), "api/v1/service_dependencies", params)
		if err != nil {
			return err
		}

		if depsMermaid {
			return printMermaid(args[0], depsDirection, data)
		}

		return printData("", data)
	},
}

var servicesTeamCmd = &cobra.Command{
	Use:   "team <team-name>",
	Short: "List services owned by a team",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("filter[team]", args[0])

		data, err := c.Get(context.Background(), "api/v2/services/definitions", params)
		if err != nil {
			return err
		}

		return printData("", extractData(data))
	},
}

func printMermaid(service, direction string, data json.RawMessage) error {
	// Parse dependency data and output Mermaid graph
	var deps map[string][]string
	if json.Unmarshal(data, &deps) != nil {
		// Try alternate format
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(data, &wrapper) == nil {
			for _, v := range wrapper {
				json.Unmarshal(v, &deps)
				break
			}
		}
	}

	mermaid := "graph " + mermaidDirection(direction) + "\n"
	if deps != nil {
		for svc, targets := range deps {
			for _, target := range targets {
				if direction == "upstream" {
					mermaid += "    " + target + " --> " + svc + "\n"
				} else {
					mermaid += "    " + svc + " --> " + target + "\n"
				}
			}
		}
	} else {
		mermaid += "    " + service + "\n"
	}

	fmt.Print(mermaid)
	return nil
}

func mermaidDirection(dir string) string {
	if dir == "upstream" {
		return "BT"
	}
	return "TD"
}
