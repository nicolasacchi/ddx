package commands

import (
	"context"
	"errors"
	"net/http"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(cloudCmd)
	cloudCmd.AddCommand(cloudAWSCmd)
	cloudCmd.AddCommand(cloudGCPCmd)
	cloudCmd.AddCommand(cloudAzureCmd)
}

var cloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Cloud integrations (AWS, GCP, Azure)",
}

// Cloud integration list endpoints. AWS and GCP prefer the v2 accounts API;
// the v1 integration endpoints are marked deprecated upstream, so they are
// only a fallback when v2 answers 404. Azure has no v2 list endpoint: the
// supported call is GET api/v1/integration/azure (the host_filters path is
// POST-only and answers 405 to a GET).
const (
	cloudAWSv2   = "api/v2/integration/aws/accounts"
	cloudAWSv1   = "api/v1/integration/aws"
	cloudGCPv2   = "api/v2/integration/gcp/accounts"
	cloudGCPv1   = "api/v1/integration/gcp"
	cloudAzureV1 = "api/v1/integration/azure"
)

// cloudGet GETs the preferred path and, if fallback is non-empty, retries the
// deprecated path when the preferred one answers 404.
func cloudGet(ctx context.Context, c *client.Client, path, fallback string) ([]byte, error) {
	data, err := c.Get(ctx, path, nil)
	if err == nil || fallback == "" {
		return data, err
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return c.Get(ctx, fallback, nil)
	}
	return data, err
}

func cloudList(path, fallback string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := cloudGet(context.Background(), c, path, fallback)
		if err != nil {
			return err
		}
		return printData("", data)
	}
}

var cloudAWSCmd = &cobra.Command{
	Use:   "aws",
	Short: "List AWS integrations",
	RunE:  cloudList(cloudAWSv2, cloudAWSv1),
}

var cloudGCPCmd = &cobra.Command{
	Use:   "gcp",
	Short: "List GCP integrations",
	RunE:  cloudList(cloudGCPv2, cloudGCPv1),
}

var cloudAzureCmd = &cobra.Command{
	Use:   "azure",
	Short: "List Azure integrations",
	RunE:  cloudList(cloudAzureV1, ""),
}
