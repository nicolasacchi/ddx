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
// bracket depth.
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
				out = append(out, part)
			}
			start = i + 1
		}
	}

	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}

	return out
}
