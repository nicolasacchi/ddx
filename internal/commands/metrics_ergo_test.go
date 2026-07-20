package commands

import (
	"testing"
)

func TestMetricsergoParseIntervalMillis(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{name: "empty means no interval", in: "", want: 0},
		{name: "all digits back-compat milliseconds", in: "300000", want: 300000},
		{name: "zero is valid", in: "0", want: 0},
		{name: "go duration seconds", in: "90s", want: 90000},
		{name: "go duration hours", in: "1h", want: 3600000},
		{name: "go duration compound", in: "1h30m", want: 5400000},
		{name: "day suffix", in: "1d", want: 86400000},
		{name: "week suffix", in: "2w", want: 1209600000},
		{name: "fractional day suffix", in: "1.5d", want: 129600000},
		{name: "whitespace trimmed", in: "  1h  ", want: 3600000},
		{name: "garbage unit rejected", in: "1x", wantErr: true},
		{name: "garbage word rejected", in: "banana", wantErr: true},
		{name: "negative rejected", in: "-5", wantErr: true},
		{name: "bare unit no number rejected", in: "d", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := metricsergoParseIntervalMillis(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("metricsergoParseIntervalMillis(%q) = %d, nil; want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("metricsergoParseIntervalMillis(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("metricsergoParseIntervalMillis(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestMetricsergoParseDayWeekDuration(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantMS int64
		wantOK bool
	}{
		{name: "one day", in: "1d", wantMS: 86400000, wantOK: true},
		{name: "two weeks", in: "2w", wantMS: 1209600000, wantOK: true},
		{name: "fractional day", in: "1.5d", wantMS: 129600000, wantOK: true},
		{name: "no unit", in: "5", wantOK: false},
		{name: "wrong unit not matched", in: "5m", wantOK: false},
		{name: "unit without number", in: "d", wantOK: false},
		{name: "empty string", in: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := metricsergoParseDayWeekDuration(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("metricsergoParseDayWeekDuration(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && d.Milliseconds() != tc.wantMS {
				t.Fatalf("metricsergoParseDayWeekDuration(%q) = %dms, want %dms", tc.in, d.Milliseconds(), tc.wantMS)
			}
		})
	}
}

func TestMetricsergoSumMetricName(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantName string
		wantOK   bool
	}{
		{name: "bare sum query", query: "sum:system.disk.used{*}", wantName: "system.disk.used", wantOK: true},
		{name: "sum with by clause", query: "sum:system.disk.used{*} by {host}", wantName: "system.disk.used", wantOK: true},
		{name: "sum with tag filter", query: "sum:system.disk.used{env:prod,region:eu}", wantName: "system.disk.used", wantOK: true},
		{name: "no trailing brace", query: "sum:system.disk.used", wantName: "system.disk.used", wantOK: true},
		{name: "avg aggregator not matched", query: "avg:system.cpu.user{*}", wantOK: false},
		{name: "max aggregator not matched", query: "max:system.cpu.user{*}", wantOK: false},
		{name: "uppercase aggregator not matched", query: "SUM:system.cpu.user{*}", wantOK: false},
		{name: "sum with nothing after it", query: "sum:{*}", wantOK: false},
		{name: "sum used mid-formula not a prefix match", query: "top(sum:a{*}, 10)", wantOK: false},
		{name: "empty query", query: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotOK := metricsergoSumMetricName(tc.query)
			if gotOK != tc.wantOK {
				t.Fatalf("metricsergoSumMetricName(%q) ok = %v, want %v", tc.query, gotOK, tc.wantOK)
			}
			if gotOK && gotName != tc.wantName {
				t.Fatalf("metricsergoSumMetricName(%q) = %q, want %q", tc.query, gotName, tc.wantName)
			}
		})
	}
}

func TestMetricsergoBuildMetricQuery(t *testing.T) {
	cases := []struct {
		name   string
		metric string
		want   string
	}{
		{name: "simple metric", metric: "system.cpu.user", want: "avg:system.cpu.user{*}"},
		{name: "empty metric", metric: "", want: "avg:{*}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := metricsergoBuildMetricQuery(tc.metric)
			if got != tc.want {
				t.Fatalf("metricsergoBuildMetricQuery(%q) = %q, want %q", tc.metric, got, tc.want)
			}
		})
	}
}
