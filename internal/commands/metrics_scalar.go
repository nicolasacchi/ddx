package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// metrics scalar — POST api/v2/query/scalar. Mirrors how `metrics query`
// builds its timeseries body (see metrics.go), but scalar queries require an
// `aggregator` field per query (MetricsScalarQuery.aggregator is required —
// verified against openapi-v2.yaml) and the response is columnar rather than
// a time series, so it gets its own flattening step.
//
// Package-prefixed identifiers (tracesscalar*) per namespace discipline —
// this file attaches a subcommand to the package-level metricsCmd (defined
// in metrics.go) from its own init(), and must never collide with sibling
// files' package-level names.
var (
	tracesscalarQueriesFlag  []string
	tracesscalarFormulasFlag []string
	tracesscalarReducerFlag  string
)

// tracesscalarValidReducers mirrors the subset of MetricsAggregator
// (openapi-v2.yaml components.schemas.MetricsAggregator) exposed by
// --reducer. The full enum also has percentile/mean/l2norm/area, but the
// task surface only calls for avg|last|max|min|sum.
var tracesscalarValidReducers = map[string]bool{
	"avg":  true,
	"last": true,
	"max":  true,
	"min":  true,
	"sum":  true,
}

func init() {
	metricsCmd.AddCommand(metricsScalarCmd)

	metricsScalarCmd.Flags().StringArrayVar(&tracesscalarQueriesFlag, "queries", nil, "Metric queries (e.g., \"avg:system.cpu.user{*} by {env}\")")
	metricsScalarCmd.Flags().StringArrayVar(&tracesscalarFormulasFlag, "formulas", nil, "Formula expressions (e.g., \"query1 / query0\")")
	metricsScalarCmd.Flags().StringVar(&tracesscalarReducerFlag, "reducer", "avg", "Aggregator applied to each query: avg|last|max|min|sum")
	metricsScalarCmd.MarkFlagRequired("queries")
}

var metricsScalarCmd = &cobra.Command{
	Use:   "scalar",
	Short: "Query scalar values (Query Value/Table/Toplist widgets) across data sources",
	Long: `Query scalar data — a single aggregated number per group, as seen on
Query Value, Table, and Toplist widgets — with optional multi-query formulas.

Unlike "metrics query" (timeseries), scalar queries require a reducer
(aggregator) per query and return one row per group instead of a bucketed
series.

Examples:
  ddx metrics scalar --queries "avg:system.cpu.user{*} by {env}" --from 1h
  ddx metrics scalar --queries "sum:trace.web.request.hits{*}","sum:trace.web.request.errors{*}" --formulas "query1 / query0" --reducer sum --from 4h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := tracesscalarValidateReducer(tracesscalarReducerFlag); err != nil {
			return err
		}

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

		queries := splitQueriesTopLevel(tracesscalarQueriesFlag)
		formulas := splitQueriesTopLevel(tracesscalarFormulasFlag)

		reducer := tracesscalarReducerFlag
		if reducer == "" {
			reducer = "avg"
		}

		body := tracesscalarBuildRequestBody(queries, formulas, reducer, from*1000, to*1000)

		data, err := c.Post(context.Background(), "api/v2/query/scalar", body)
		if err != nil {
			return err
		}

		rows, err := tracesscalarFlattenScalarResponse(data)
		if err != nil {
			return err
		}

		out, err := json.Marshal(rows)
		if err != nil {
			return err
		}

		return printData("", out)
	},
}

// tracesscalarValidateReducer rejects a --reducer value outside the
// supported set before any request is built or sent.
func tracesscalarValidateReducer(r string) error {
	if r == "" {
		return nil
	}
	if !tracesscalarValidReducers[r] {
		return fmt.Errorf("invalid --reducer %q: must be one of avg, last, max, min, sum", r)
	}
	return nil
}

// tracesscalarBuildQueries builds the ScalarQuery (MetricsScalarQuery)
// objects for a scalar_request body: one per query string, named query0,
// query1, ... for use in --formulas, sharing the same "metrics" data_source
// and aggregator as the other queries in the request.
func tracesscalarBuildQueries(queries []string, aggregator string) []map[string]any {
	out := make([]map[string]any, len(queries))
	for i, q := range queries {
		out[i] = map[string]any{
			"name":        "query" + strconv.Itoa(i),
			"data_source": "metrics",
			"aggregator":  aggregator,
			"query":       q,
		}
	}
	return out
}

// tracesscalarBuildFormulas builds the QueryFormula list, or nil when no
// formulas were requested (so the "formulas" key is omitted from the body
// entirely, matching metrics.go's convention for metricsQueryCmd).
func tracesscalarBuildFormulas(formulas []string) []map[string]any {
	if len(formulas) == 0 {
		return nil
	}
	out := make([]map[string]any, len(formulas))
	for i, f := range formulas {
		out[i] = map[string]any{"formula": f}
	}
	return out
}

// tracesscalarBuildRequestBody constructs the full scalar_request body
// (components.schemas.ScalarFormulaQueryRequest) given already-split query
// and formula strings, a reducer/aggregator, and from/to in milliseconds
// since the Unix epoch (ScalarFormulaRequestAttributes.from/to).
func tracesscalarBuildRequestBody(queries, formulas []string, aggregator string, fromMs, toMs int64) map[string]any {
	attrs := map[string]any{
		"from":    fromMs,
		"to":      toMs,
		"queries": tracesscalarBuildQueries(queries, aggregator),
	}
	if fs := tracesscalarBuildFormulas(formulas); fs != nil {
		attrs["formulas"] = fs
	}
	return map[string]any{
		"data": map[string]any{
			"type":       "scalar_request",
			"attributes": attrs,
		},
	}
}

// tracesscalarColumnRaw is one entry of ScalarFormulaResponseAtrributes.columns
// (components.schemas.ScalarColumn, a oneOf GroupScalarColumn/DataScalarColumn
// discriminated by "type": "group" | "number"). Values is left raw because
// its shape differs by column type: [][]string for group columns, []*float64
// (nullable) for number columns.
type tracesscalarColumnRaw struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Values json.RawMessage `json:"values"`
}

type tracesscalarResponseRaw struct {
	Data struct {
		Attributes struct {
			Columns []tracesscalarColumnRaw `json:"columns"`
		} `json:"attributes"`
	} `json:"data"`
}

// tracesscalarDataColumn is a decoded "number"-type column: the values for
// one query/formula, indexed in parallel with the group columns' rows.
type tracesscalarDataColumn struct {
	Name   string
	Values []*float64
}

// tracesscalarFlattenScalarResponse flattens the columnar scalar_response
// (one column per query/formula plus one column per group-by tag, all
// value arrays running in parallel by row index) into
// [{"name":..., "tags": {...}, "value": ...}] — one row per (data column,
// group index). "tags" is omitted entirely when the query had no "by"
// clause (no group columns in the response).
func tracesscalarFlattenScalarResponse(raw json.RawMessage) ([]map[string]any, error) {
	var resp tracesscalarResponseRaw
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse scalar response: %w", err)
	}

	var groupNames []string
	groupValues := map[string][]string{}
	var dataCols []tracesscalarDataColumn
	rowCount := 0

	for _, col := range resp.Data.Attributes.Columns {
		switch col.Type {
		case "group":
			var vals [][]string
			if err := json.Unmarshal(col.Values, &vals); err != nil {
				continue
			}
			joined := make([]string, len(vals))
			for i, v := range vals {
				joined[i] = strings.Join(v, ",")
			}
			groupNames = append(groupNames, col.Name)
			groupValues[col.Name] = joined
			if len(joined) > rowCount {
				rowCount = len(joined)
			}
		case "number":
			var vals []*float64
			if err := json.Unmarshal(col.Values, &vals); err != nil {
				continue
			}
			dataCols = append(dataCols, tracesscalarDataColumn{Name: col.Name, Values: vals})
			if len(vals) > rowCount {
				rowCount = len(vals)
			}
		}
	}

	var rows []map[string]any
	for _, dc := range dataCols {
		for i := 0; i < rowCount; i++ {
			row := map[string]any{"name": dc.Name}
			if len(groupNames) > 0 {
				tags := map[string]string{}
				for _, gn := range groupNames {
					if vs := groupValues[gn]; i < len(vs) {
						tags[gn] = vs[i]
					}
				}
				row["tags"] = tags
			}
			var value any
			if i < len(dc.Values) && dc.Values[i] != nil {
				value = *dc.Values[i]
			}
			row["value"] = value
			rows = append(rows, row)
		}
	}
	if rows == nil {
		rows = []map[string]any{}
	}

	return rows, nil
}
