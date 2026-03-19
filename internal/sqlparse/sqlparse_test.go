package sqlparse

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantErr   bool
		errSubstr string
		check     func(*testing.T, *ParsedQuery)
	}{
		{
			name: "simple count",
			sql:  "SELECT COUNT(*) FROM logs",
			check: func(t *testing.T, q *ParsedQuery) {
				if len(q.Columns) != 1 {
					t.Fatalf("expected 1 column, got %d", len(q.Columns))
				}
				if q.Columns[0].Aggregate != "count" {
					t.Errorf("expected aggregate=count, got %q", q.Columns[0].Aggregate)
				}
				if q.Columns[0].Metric != "*" {
					t.Errorf("expected metric=*, got %q", q.Columns[0].Metric)
				}
			},
		},
		{
			name: "count with where",
			sql:  "SELECT COUNT(*) FROM logs WHERE status = 'error'",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "status:error" {
					t.Errorf("expected filter=status:error, got %q", q.Filter)
				}
			},
		},
		{
			name: "count grouped",
			sql:  "SELECT service, COUNT(*) FROM logs WHERE status = 'error' GROUP BY service ORDER BY COUNT(*) DESC LIMIT 10",
			check: func(t *testing.T, q *ParsedQuery) {
				if len(q.Columns) != 2 {
					t.Fatalf("expected 2 columns, got %d", len(q.Columns))
				}
				if q.Columns[0].Field != "service" {
					t.Errorf("expected field=service, got %q", q.Columns[0].Field)
				}
				if q.Columns[1].Aggregate != "count" {
					t.Errorf("expected aggregate=count, got %q", q.Columns[1].Aggregate)
				}
				if q.Filter != "status:error" {
					t.Errorf("expected filter=status:error, got %q", q.Filter)
				}
				if len(q.GroupBy) != 1 || q.GroupBy[0] != "service" {
					t.Errorf("expected group_by=[service], got %v", q.GroupBy)
				}
				if q.Limit != 10 {
					t.Errorf("expected limit=10, got %d", q.Limit)
				}
				if q.SortOrder != "desc" {
					t.Errorf("expected sort=desc, got %q", q.SortOrder)
				}
			},
		},
		{
			name: "avg with @field",
			sql:  "SELECT @http.status_code, AVG(@duration) FROM logs GROUP BY @http.status_code",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Columns[0].Field != "@http.status_code" {
					t.Errorf("expected field=@http.status_code, got %q", q.Columns[0].Field)
				}
				if q.Columns[1].Aggregate != "avg" || q.Columns[1].Metric != "@duration" {
					t.Errorf("expected avg(@duration), got %s(%s)", q.Columns[1].Aggregate, q.Columns[1].Metric)
				}
				if len(q.GroupBy) != 1 || q.GroupBy[0] != "@http.status_code" {
					t.Errorf("expected group_by=[@http.status_code], got %v", q.GroupBy)
				}
			},
		},
		{
			name: "where with AND",
			sql:  "SELECT COUNT(*) FROM logs WHERE service = 'web' AND status = 'error'",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "service:web status:error" {
					t.Errorf("expected filter=service:web status:error, got %q", q.Filter)
				}
			},
		},
		{
			name: "where with IN",
			sql:  "SELECT service, COUNT(*) FROM logs WHERE service IN ('web', 'api') GROUP BY service",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "service:(web OR api)" {
					t.Errorf("expected filter=service:(web OR api), got %q", q.Filter)
				}
			},
		},
		{
			name: "where with comparison",
			sql:  "SELECT COUNT(*) FROM logs WHERE @duration > 1000",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "@duration:>1000" {
					t.Errorf("expected filter=@duration:>1000, got %q", q.Filter)
				}
			},
		},
		{
			name: "where with != ",
			sql:  "SELECT COUNT(*) FROM logs WHERE status != 'info'",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "-status:info" {
					t.Errorf("expected filter=-status:info, got %q", q.Filter)
				}
			},
		},
		{
			name: "multiple group by",
			sql:  "SELECT service, host, COUNT(*) FROM logs GROUP BY service, host",
			check: func(t *testing.T, q *ParsedQuery) {
				if len(q.GroupBy) != 2 {
					t.Fatalf("expected 2 group_by, got %d", len(q.GroupBy))
				}
				if q.GroupBy[0] != "service" || q.GroupBy[1] != "host" {
					t.Errorf("expected [service, host], got %v", q.GroupBy)
				}
			},
		},
		{
			name: "with alias",
			sql:  "SELECT service, COUNT(*) AS cnt FROM logs GROUP BY service",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Columns[1].Alias != "cnt" {
					t.Errorf("expected alias=cnt, got %q", q.Columns[1].Alias)
				}
			},
		},
		{
			name: "asc order",
			sql:  "SELECT service, COUNT(*) FROM logs GROUP BY service ORDER BY COUNT(*) ASC",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.SortOrder != "asc" {
					t.Errorf("expected sort=asc, got %q", q.SortOrder)
				}
			},
		},
		{
			name: "sum aggregate",
			sql:  "SELECT SUM(@bytes) FROM logs",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Columns[0].Aggregate != "sum" || q.Columns[0].Metric != "@bytes" {
					t.Errorf("expected sum(@bytes), got %s(%s)", q.Columns[0].Aggregate, q.Columns[0].Metric)
				}
			},
		},
		{
			name:      "JOIN rejected",
			sql:       "SELECT a FROM logs JOIN other ON logs.id = other.id",
			wantErr:   true,
			errSubstr: "JOIN",
		},
		{
			name:      "HAVING rejected",
			sql:       "SELECT service, COUNT(*) FROM logs GROUP BY service HAVING COUNT(*) > 10",
			wantErr:   true,
			errSubstr: "HAVING",
		},
		{
			name: "no where clause",
			sql:  "SELECT service, COUNT(*) FROM logs GROUP BY service LIMIT 5",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "" {
					t.Errorf("expected empty filter, got %q", q.Filter)
				}
				if q.Limit != 5 {
					t.Errorf("expected limit=5, got %d", q.Limit)
				}
			},
		},
		{
			name: "compound where with IN and comparison",
			sql:  "SELECT service, COUNT(*) FROM logs WHERE service IN ('web', 'api') AND @duration > 1000 GROUP BY service",
			check: func(t *testing.T, q *ParsedQuery) {
				if q.Filter != "service:(web OR api) @duration:>1000" {
					t.Errorf("expected compound filter, got %q", q.Filter)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := Parse(tt.sql)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, q)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
