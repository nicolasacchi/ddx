package commands

import "strings"

// splitQueriesTopLevel splits each input string on commas that sit outside
// curly braces, parentheses, single quotes, and double quotes, then trims
// whitespace and drops empty entries. It is meant for flags like
// --queries/--formulas where a caller may either pass one query per flag
// occurrence (already split — passes through unchanged) or a single
// comma-joined string containing multiple Datadog metric queries whose own
// tag filters ("avg:a{x:1,y:2}") also contain commas that must NOT be treated
// as separators.
//
// Example:
//
//	splitQueriesTopLevel([]string{"avg:a{x:1,y:2},avg:b{*}"})
//	  -> []string{"avg:a{x:1,y:2}", "avg:b{*}"}
func splitQueriesTopLevel(vals []string) []string {
	var out []string
	for _, v := range vals {
		out = append(out, helpersSplitTopLevel(v)...)
	}
	return out
}

// helpersSplitTopLevel splits a single string on top-level commas — commas
// that are not nested inside {}, (), '...' or "...". Nesting depth is tracked
// with a single counter (braces and parens share it: a "simple depth
// counter" is sufficient here, since we only need to know "are we inside
// some bracketed group", not which kind). Quotes toggle a separate state so
// commas inside quoted strings are never treated as separators, regardless of
// bracket depth. Each resulting part then has one matching outer pair of
// double quotes stripped, if present (see helpersUnwrapOuterQuotes) — the
// CSV-escape-hatch back-compat this restores.
func helpersSplitTopLevel(s string) []string {
	var out []string
	depth := 0
	var quote rune
	start := 0

	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '{' || r == '(':
			depth++
		case r == '}' || r == ')':
			if depth > 0 {
				depth--
			}
		case r == ',' && depth == 0:
			if part := strings.TrimSpace(s[start:i]); part != "" {
				out = append(out, helpersUnwrapOuterQuotes(part))
			}
			start = i + 1
		}
	}

	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, helpersUnwrapOuterQuotes(part))
	}

	return out
}

// helpersUnwrapOuterQuotes strips one matching outer pair of double quotes
// from s, leaving any inner quotes untouched. Restores a back-compat escape
// hatch: on main, --queries/--formulas used StringSliceVar (CSV parsing),
// which stripped double quotes for you, so callers could shield a query's own
// top-level commas with e.g. --queries '"avg:m{a:1,b:2}"'. StringArrayVar +
// splitQueriesTopLevel already protects those commas via bracket/quote
// tracking without needing the quotes at all, but passes whole flag values
// through verbatim — so without this, quotes used out of habit (or copied
// from an old example) would reach the Datadog API and 400. A lone
// unbalanced quote (length 1) has no pair to strip and is left alone.
func helpersUnwrapOuterQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
