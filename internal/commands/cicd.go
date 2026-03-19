package commands

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
)

var cicdQuery string

func init() {
	rootCmd.AddCommand(cicdCmd)
	cicdCmd.AddCommand(cicdPipelinesCmd)
	cicdCmd.AddCommand(cicdTestsCmd)

	cicdPipelinesCmd.Flags().StringVar(&cicdQuery, "query", "*", "Pipeline query")
	cicdTestsCmd.Flags().StringVar(&cicdQuery, "query", "*", "Test query")
}

var cicdCmd = &cobra.Command{
	Use:   "cicd",
	Short: "CI/CD pipeline and test insights",
}

var cicdPipelinesCmd = &cobra.Command{
	Use:   "pipelines",
	Short: "List CI pipelines",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		if cicdQuery != "*" {
			params.Set("filter[query]", cicdQuery)
		}
		data, err := c.Get(context.Background(), "api/v2/ci/pipelines/events", params)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var cicdTestsCmd = &cobra.Command{
	Use:   "tests",
	Short: "List CI test events",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		params := url.Values{}
		if cicdQuery != "*" {
			params.Set("filter[query]", cicdQuery)
		}
		data, err := c.Get(context.Background(), "api/v2/ci/tests/events", params)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}
