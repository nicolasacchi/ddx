package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/term"
)

type FormatFunc func(any) string

type ColumnDef struct {
	Header string
	Key    string
	Format FormatFunc
}

var commandColumns = map[string][]ColumnDef{
	"logs.search": {
		{Header: "TIMESTAMP", Key: "timestamp"},
		{Header: "STATUS", Key: "status"},
		{Header: "SERVICE", Key: "service"},
		{Header: "MESSAGE", Key: "message", Format: truncate80},
	},
	"monitors.list": {
		{Header: "ID", Key: "id"},
		{Header: "NAME", Key: "name"},
		{Header: "STATUS", Key: "overall_state"},
		{Header: "TYPE", Key: "type"},
	},
	"monitors.search": {
		{Header: "ID", Key: "id"},
		{Header: "NAME", Key: "name"},
		{Header: "STATUS", Key: "overall_state"},
		{Header: "TYPE", Key: "type"},
	},
	"incidents.list": {
		{Header: "ID", Key: "id"},
		{Header: "TITLE", Key: "title"},
		{Header: "SEVERITY", Key: "severity"},
		{Header: "STATE", Key: "state"},
		{Header: "CREATED", Key: "created"},
	},
	"error-tracking.search": {
		{Header: "ID", Key: "id"},
		{Header: "TYPE", Key: "error_type"},
		{Header: "SERVICE", Key: "service"},
		{Header: "COUNT", Key: "total_count"},
		{Header: "MESSAGE", Key: "error_message", Format: truncate80},
	},
	"metrics.list": {
		{Header: "NAME", Key: "id"},
	},
	"dashboards.list": {
		{Header: "ID", Key: "id"},
		{Header: "TITLE", Key: "title"},
		{Header: "AUTHOR", Key: "author_handle"},
	},
	"dashboards.search": {
		{Header: "ID", Key: "id"},
		{Header: "TITLE", Key: "title"},
		{Header: "AUTHOR", Key: "author_handle"},
	},
	"rum.apps": {
		{Header: "ID", Key: "id"},
		{Header: "NAME", Key: "name"},
		{Header: "TYPE", Key: "type"},
	},
	"config.list": {
		{Header: "NAME", Key: "name"},
		{Header: "API KEY", Key: "api_key"},
		{Header: "APP KEY", Key: "app_key"},
		{Header: "SITE", Key: "site"},
		{Header: "DEFAULT", Key: "default"},
	},
}

func printTable(command string, data json.RawMessage) error {
	columns, ok := commandColumns[command]
	if !ok {
		return fmt.Errorf("no table definition for %s", command)
	}

	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		var single map[string]any
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return fmt.Errorf("cannot render as table")
		}
		rows = []map[string]any{single}
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	if term.IsTerminal(int(os.Stdout.Fd())) {
		t.SetStyle(table.StyleLight)
	} else {
		t.SetStyle(table.StyleDefault)
	}

	header := make(table.Row, len(columns))
	for i, col := range columns {
		header[i] = col.Header
	}
	t.AppendHeader(header)

	for _, row := range rows {
		r := make(table.Row, len(columns))
		for i, col := range columns {
			if col.Format != nil {
				r[i] = col.Format(row[col.Key])
			} else {
				r[i] = formatValue(row[col.Key])
			}
		}
		t.AppendRow(r)
	}

	t.Render()
	return nil
}

func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(parts, ", ")
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func truncate80(v any) string {
	s := formatValue(v)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
