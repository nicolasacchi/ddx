package commands

import "testing"

func TestDriftBuildNetworkAggregateParams(t *testing.T) {
	tests := []struct {
		name    string
		from    int64
		to      int64
		groupBy string
		tags    string
		query   string
		limit   int
		want    map[string]string // param -> expected value; absent key means "must not be set"
		absent  []string
	}{
		{
			name:    "from/to/group_by always set, no tags or query",
			from:    1000,
			to:      2000,
			groupBy: "client_service,server_service",
			want: map[string]string{
				"from":     "1000",
				"to":       "2000",
				"group_by": "client_service,server_service",
			},
			absent: []string{"tags", "query", "limit"},
		},
		{
			name:    "tags set when query is empty",
			from:    1,
			to:      2,
			groupBy: "network.dns_query",
			tags:    "env:prod,service:web",
			want: map[string]string{
				"from":     "1",
				"to":       "2",
				"group_by": "network.dns_query",
				"tags":     "env:prod,service:web",
			},
			absent: []string{"query"},
		},
		{
			name:    "query takes precedence over tags",
			from:    1,
			to:      2,
			groupBy: "client_service",
			tags:    "env:prod",
			query:   "server_service:checkout",
			want: map[string]string{
				"query": "server_service:checkout",
			},
			absent: []string{"tags"},
		},
		{
			name: "empty group_by omitted",
			from: 1,
			to:   2,
			want: map[string]string{
				"from": "1",
				"to":   "2",
			},
			absent: []string{"group_by", "tags", "query", "limit"},
		},
		{
			name:  "positive limit included",
			from:  1,
			to:    2,
			limit: 500,
			want: map[string]string{
				"limit": "500",
			},
		},
		{
			name:   "zero/negative limit omitted",
			from:   1,
			to:     2,
			limit:  0,
			absent: []string{"limit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := driftBuildNetworkAggregateParams(tt.from, tt.to, tt.groupBy, tt.tags, tt.query, tt.limit)

			for k, v := range tt.want {
				if got := params.Get(k); got != v {
					t.Errorf("params[%q] = %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.absent {
				if params.Has(k) {
					t.Errorf("params[%q] = %q, want absent", k, params.Get(k))
				}
			}
		})
	}
}
