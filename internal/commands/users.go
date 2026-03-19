package commands

import (
	"context"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(usersCmd)
	usersCmd.AddCommand(usersListCmd)
	usersCmd.AddCommand(usersGetCmd)
}

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "List and inspect users",
}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/users", nil)
		if err != nil {
			return err
		}
		return printData("", extractData(data))
	},
}

var usersGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get user details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		data, err := c.Get(context.Background(), "api/v2/users/"+args[0], nil)
		if err != nil {
			return err
		}
		return printData("", data)
	},
}
