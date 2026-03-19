package output

import (
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/term"
)

// IsJSON returns true if output should be JSON (non-TTY, --json flag, or --jq set).
func IsJSON(jsonFlag bool, jqFilter string) bool {
	if jsonFlag || jqFilter != "" {
		return true
	}
	return !term.IsTerminal(int(os.Stdout.Fd()))
}

// PrintData outputs data as JSON or table depending on context.
func PrintData(command string, data json.RawMessage, jsonMode bool, jqFilter string) error {
	if !jsonMode {
		if err := printTable(command, data); err == nil {
			return nil
		}
	}

	if jqFilter != "" {
		filtered, err := ApplyFilter(data, jqFilter)
		if err != nil {
			return err
		}
		data = filtered
	}

	return printJSON(data)
}

// PrintError outputs a structured error to stdout.
func PrintError(errMsg string, statusCode int) {
	errObj := map[string]any{"ok": false, "error": errMsg}
	if statusCode > 0 {
		errObj["status_code"] = statusCode
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(errObj)
}

func printJSON(data json.RawMessage) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}

// PrintJSONValue prints any Go value as formatted JSON.
func PrintJSONValue(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}
