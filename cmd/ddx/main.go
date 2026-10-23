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
		// Data was already streamed to stdout: keep stdout a valid document and
		// report on stderr only (exit code unchanged).
		var streamed *commands.StreamedError
		stdoutClean := !errors.As(err, &streamed)

		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			if stdoutClean {
				output.PrintError(apiErr.Error(), apiErr.StatusCode)
			}
			fmt.Fprintln(os.Stderr, "Error:", apiErr.Error())
			if apiErr.Hint != "" {
				fmt.Fprintln(os.Stderr, "Hint:", apiErr.Hint)
			}
			os.Exit(apiErr.ExitCode())
		}
		if stdoutClean {
			output.PrintError(err.Error(), 0)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
