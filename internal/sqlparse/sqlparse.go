package sqlparse

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ParsedQuery represents a parsed SQL SELECT statement.
type ParsedQuery struct {
	Columns   []Column // SELECT columns
	Filter    string   // WHERE → Datadog filter query
	GroupBy   []string // GROUP BY fields
	Limit     int      // LIMIT value (0 = use default)
	SortOrder string   // "desc" or "asc" (from ORDER BY)
}

// Column represents a SELECT column — either a plain field or an aggregate.
type Column struct {
	Field     string // field name (empty for aggregate-only like COUNT(*))
	Aggregate string // "count", "avg", "sum", "min", "max" or ""
	Metric    string // aggregate argument: "@duration", "*", etc.
	Alias     string // AS alias
}

// HasAggregate returns the first aggregate column, if any.
func (q *ParsedQuery) HasAggregate() *Column {
	for i := range q.Columns {
		if q.Columns[i].Aggregate != "" {
			return &q.Columns[i]
		}
	}
	return nil
}

// PlainFields returns non-aggregate column field names.
func (q *ParsedQuery) PlainFields() []string {
	var fields []string
	for _, c := range q.Columns {
		if c.Aggregate == "" && c.Field != "" {
			fields = append(fields, c.Field)
		}
	}
	return fields
}

// token types
type tokenKind int

const (
	tkWord   tokenKind = iota // keyword or identifier
	tkNumber                  // integer or float
	tkString                  // 'single-quoted'
	tkOp                      // = > < >= <= != <>
	tkStar                    // *
	tkComma                   // ,
	tkLParen                  // (
	tkRParen                  // )
	tkEOF
)

type token struct {
	kind tokenKind
	val  string
}

// Parse parses a SQL SELECT statement into a ParsedQuery.
func Parse(sql string) (*ParsedQuery, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	return p.parse()
}

// --- Tokenizer ---

func tokenize(sql string) ([]token, error) {
	var tokens []token
	runes := []rune(sql)
	i := 0

	for i < len(runes) {
		ch := runes[i]

		// Skip whitespace
		if unicode.IsSpace(ch) {
			i++
			continue
		}

		// Single-quoted string
		if ch == '\'' {
			j := i + 1
			for j < len(runes) && runes[j] != '\'' {
				if runes[j] == '\\' {
					j++ // skip escaped char
				}
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated string at position %d", i)
			}
			tokens = append(tokens, token{tkString, string(runes[i+1 : j])})
			i = j + 1
			continue
		}

		// Operators
		if ch == '=' {
			tokens = append(tokens, token{tkOp, "="})
			i++
			continue
		}
		if ch == '!' && i+1 < len(runes) && runes[i+1] == '=' {
			tokens = append(tokens, token{tkOp, "!="})
			i += 2
			continue
		}
		if ch == '<' {
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, token{tkOp, "<="})
				i += 2
			} else if i+1 < len(runes) && runes[i+1] == '>' {
				tokens = append(tokens, token{tkOp, "!="})
				i += 2
			} else {
				tokens = append(tokens, token{tkOp, "<"})
				i++
			}
			continue
		}
		if ch == '>' {
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, token{tkOp, ">="})
				i += 2
			} else {
				tokens = append(tokens, token{tkOp, ">"})
				i++
			}
			continue
		}

		// Punctuation
		if ch == '*' {
			tokens = append(tokens, token{tkStar, "*"})
			i++
			continue
		}
		if ch == ',' {
			tokens = append(tokens, token{tkComma, ","})
			i++
			continue
		}
		if ch == '(' {
			tokens = append(tokens, token{tkLParen, "("})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, token{tkRParen, ")"})
			i++
			continue
		}

		// Number (integer or float)
		if unicode.IsDigit(ch) {
			j := i
			for j < len(runes) && (unicode.IsDigit(runes[j]) || runes[j] == '.') {
				j++
			}
			tokens = append(tokens, token{tkNumber, string(runes[i:j])})
			i = j
			continue
		}

		// Word: identifier or keyword (allow @, ., _ in identifiers)
		if unicode.IsLetter(ch) || ch == '@' || ch == '_' {
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '@' || runes[j] == '.' || runes[j] == '_' || runes[j] == '-') {
				j++
			}
			tokens = append(tokens, token{tkWord, string(runes[i:j])})
			i = j
			continue
		}

		// Double-quoted identifier
		if ch == '"' {
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated identifier at position %d", i)
			}
			tokens = append(tokens, token{tkWord, string(runes[i+1 : j])})
			i = j + 1
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at position %d", string(ch), i)
	}

	tokens = append(tokens, token{tkEOF, ""})
	return tokens, nil
}

// --- Parser ---

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{tkEOF, ""}
	}
	return p.tokens[p.pos]
}

func (p *parser) next() token {
	t := p.peek()
	if t.kind != tkEOF {
		p.pos++
	}
	return t
}

func (p *parser) expectWord(word string) error {
	t := p.next()
	if t.kind != tkWord || !strings.EqualFold(t.val, word) {
		return fmt.Errorf("expected %q, got %q", word, t.val)
	}
	return nil
}

func (p *parser) isWord(word string) bool {
	t := p.peek()
	return t.kind == tkWord && strings.EqualFold(t.val, word)
}

func (p *parser) parse() (*ParsedQuery, error) {
	q := &ParsedQuery{SortOrder: "desc"}

	// Check for unsupported features
	for _, t := range p.tokens {
		if t.kind == tkWord {
			upper := strings.ToUpper(t.val)
			switch upper {
			case "JOIN", "INNER", "LEFT", "RIGHT", "OUTER", "CROSS":
				return nil, fmt.Errorf("JOIN is not supported — use Datadog MCP analyze_datadog_logs for complex SQL")
			case "HAVING":
				return nil, fmt.Errorf("HAVING is not supported — use Datadog MCP analyze_datadog_logs for complex SQL")
			case "UNION":
				return nil, fmt.Errorf("UNION is not supported — use Datadog MCP analyze_datadog_logs for complex SQL")
			case "WITH":
				return nil, fmt.Errorf("CTEs (WITH) are not supported — use Datadog MCP analyze_datadog_logs for complex SQL")
			}
		}
	}

	// SELECT
	if err := p.expectWord("SELECT"); err != nil {
		return nil, fmt.Errorf("query must start with SELECT")
	}

	columns, err := p.parseColumns()
	if err != nil {
		return nil, err
	}
	q.Columns = columns

	// FROM (required but table name is ignored)
	if p.isWord("FROM") {
		p.next() // FROM
		p.next() // table name (logs)
	}

	// WHERE
	if p.isWord("WHERE") {
		p.next() // WHERE
		filter, err := p.parseWhere()
		if err != nil {
			return nil, err
		}
		q.Filter = filter
	}

	// GROUP BY
	if p.isWord("GROUP") {
		p.next() // GROUP
		if err := p.expectWord("BY"); err != nil {
			return nil, err
		}
		groupBy, err := p.parseGroupBy()
		if err != nil {
			return nil, err
		}
		q.GroupBy = groupBy
	}

	// ORDER BY
	if p.isWord("ORDER") {
		p.next() // ORDER
		if err := p.expectWord("BY"); err != nil {
			return nil, err
		}
		// Skip the order expression (e.g., COUNT(*), field name)
		for {
			t := p.peek()
			if t.kind == tkEOF || p.isWord("LIMIT") || p.isWord("DESC") || p.isWord("ASC") {
				break
			}
			p.next()
		}
		if p.isWord("DESC") {
			p.next()
			q.SortOrder = "desc"
		} else if p.isWord("ASC") {
			p.next()
			q.SortOrder = "asc"
		}
	}

	// LIMIT
	if p.isWord("LIMIT") {
		p.next() // LIMIT
		t := p.next()
		if t.kind != tkNumber {
			return nil, fmt.Errorf("LIMIT requires a number, got %q", t.val)
		}
		n, err := strconv.Atoi(t.val)
		if err != nil {
			return nil, fmt.Errorf("invalid LIMIT value: %s", t.val)
		}
		q.Limit = n
	}

	return q, nil
}

func (p *parser) parseColumns() ([]Column, error) {
	var cols []Column
	for {
		col, err := p.parseOneColumn()
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)

		if p.peek().kind != tkComma {
			break
		}
		p.next() // consume comma
	}
	return cols, nil
}

func (p *parser) parseOneColumn() (Column, error) {
	t := p.peek()

	// Star alone: SELECT *
	if t.kind == tkStar {
		p.next()
		return Column{Field: "*"}, nil
	}

	// Check if this is an aggregate function
	if t.kind == tkWord && isAggregate(t.val) {
		agg := strings.ToLower(t.val)
		p.next() // function name

		if p.peek().kind != tkLParen {
			// Not a function call — it's a field name
			col := Column{Field: t.val}
			if p.isWord("AS") {
				p.next()
				col.Alias = p.next().val
			}
			return col, nil
		}
		p.next() // (

		// Read argument
		metric := ""
		if p.peek().kind == tkStar {
			metric = "*"
			p.next()
		} else {
			metric = p.next().val
		}

		if p.peek().kind != tkRParen {
			return Column{}, fmt.Errorf("expected ) after aggregate argument")
		}
		p.next() // )

		col := Column{Aggregate: agg, Metric: metric}
		if p.isWord("AS") {
			p.next()
			col.Alias = p.next().val
		}
		return col, nil
	}

	// Plain field
	if t.kind == tkWord {
		p.next()
		col := Column{Field: t.val}
		if p.isWord("AS") {
			p.next()
			col.Alias = p.next().val
		}
		return col, nil
	}

	return Column{}, fmt.Errorf("unexpected token in SELECT: %q", t.val)
}

func isAggregate(s string) bool {
	switch strings.ToUpper(s) {
	case "COUNT", "AVG", "SUM", "MIN", "MAX":
		return true
	}
	return false
}

func (p *parser) parseWhere() (string, error) {
	var parts []string

	for {
		t := p.peek()
		if t.kind == tkEOF || p.isWord("GROUP") || p.isWord("ORDER") || p.isWord("LIMIT") {
			break
		}

		cond, err := p.parseCondition()
		if err != nil {
			return "", err
		}
		parts = append(parts, cond)

		// Check for AND/OR
		if p.isWord("AND") {
			p.next()
			continue
		}
		if p.isWord("OR") {
			p.next()
			// Wrap in OR
			next, err := p.parseCondition()
			if err != nil {
				return "", err
			}
			last := parts[len(parts)-1]
			parts[len(parts)-1] = "(" + last + " OR " + next + ")"
			continue
		}

		break
	}

	return strings.Join(parts, " "), nil
}

func (p *parser) parseCondition() (string, error) {
	// field op value
	fieldTok := p.next()
	if fieldTok.kind != tkWord {
		return "", fmt.Errorf("expected field name in WHERE, got %q", fieldTok.val)
	}
	field := fieldTok.val

	// IN
	if p.isWord("IN") {
		p.next() // IN
		if p.peek().kind != tkLParen {
			return "", fmt.Errorf("expected ( after IN")
		}
		p.next() // (
		var vals []string
		for {
			v := p.next()
			if v.kind == tkString || v.kind == tkNumber || v.kind == tkWord {
				vals = append(vals, v.val)
			}
			if p.peek().kind == tkComma {
				p.next()
				continue
			}
			break
		}
		if p.peek().kind == tkRParen {
			p.next()
		}
		return field + ":(" + strings.Join(vals, " OR ") + ")", nil
	}

	// LIKE
	if p.isWord("LIKE") {
		p.next() // LIKE
		v := p.next()
		pattern := v.val
		// Convert SQL LIKE to Datadog wildcard: % → *
		pattern = strings.ReplaceAll(pattern, "%", "*")
		return pattern, nil
	}

	// NOT
	if p.isWord("NOT") {
		p.next()
		if p.isWord("IN") {
			// NOT IN — negate
			p.next()
			if p.peek().kind == tkLParen {
				p.next()
			}
			var vals []string
			for {
				v := p.next()
				if v.kind == tkString || v.kind == tkNumber || v.kind == tkWord {
					vals = append(vals, v.val)
				}
				if p.peek().kind == tkComma {
					p.next()
					continue
				}
				break
			}
			if p.peek().kind == tkRParen {
				p.next()
			}
			return "-" + field + ":(" + strings.Join(vals, " OR ") + ")", nil
		}
	}

	// Standard operator
	op := p.next()
	if op.kind != tkOp {
		return "", fmt.Errorf("expected operator after %s, got %q", field, op.val)
	}

	val := p.next()
	value := val.val

	switch op.val {
	case "=":
		return field + ":" + value, nil
	case "!=":
		return "-" + field + ":" + value, nil
	case ">":
		return field + ":>" + value, nil
	case ">=":
		return field + ":>=" + value, nil
	case "<":
		return field + ":<" + value, nil
	case "<=":
		return field + ":<=" + value, nil
	default:
		return "", fmt.Errorf("unsupported operator %q", op.val)
	}
}

func (p *parser) parseGroupBy() ([]string, error) {
	var fields []string
	for {
		t := p.next()
		if t.kind != tkWord {
			return nil, fmt.Errorf("expected field name in GROUP BY, got %q", t.val)
		}
		fields = append(fields, t.val)

		if p.peek().kind != tkComma {
			break
		}
		p.next() // comma
	}
	return fields, nil
}
