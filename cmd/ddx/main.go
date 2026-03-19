package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nicolasacchi/ddx/internal/client"
	"github.com/nicolasacchi/ddx/internal/commands"
	"github.com/nicolasacchi/ddx/internal/output"
)

var version = "dev"

func main() {
	commands.SetVersion(version)
	if err := commands.Execute(); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			output.PrintError(apiErr.Error(), apiErr.StatusCode)
			fmt.Fprintln(os.Stderr, "Error:", apiErr.Error())
			if apiErr.Hint != "" {
				fmt.Fprintln(os.Stderr, "Hint:", apiErr.Hint)
			}
			os.Exit(apiErr.ExitCode())
		}
		output.PrintError(err.Error(), 0)
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
