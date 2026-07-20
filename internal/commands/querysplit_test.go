package commands

import (
	"reflect"
	"testing"
)

func TestSplitQueriesTopLevel(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "plain single",
			in:   []string{"avg:system.cpu.user{*}"},
			want: []string{"avg:system.cpu.user{*}"},
		},
		{
			name: "top-level split",
			in:   []string{"avg:a{x:1,y:2},avg:b{*}"},
			want: []string{"avg:a{x:1,y:2}", "avg:b{*}"},
		},
		{
			name: "braces with commas",
			in:   []string{"avg:a{env:prod,region:eu},avg:b{env:prod,region:us}"},
			want: []string{"avg:a{env:prod,region:eu}", "avg:b{env:prod,region:us}"},
		},
		{
			name: "parens with commas",
			in:   []string{"top(sum:a{*},10),avg:b{*}"},
			want: []string{"top(sum:a{*},10)", "avg:b{*}"},
		},
		{
			name: "quoted commas",
			in:   []string{`a:"x,y",b:"z"`},
			want: []string{`a:"x,y"`, `b:"z"`},
		},
		{
			name: "nested braces-in-parens",
			in:   []string{"top(sum:a{x:1,y:2},10),avg:b{*}"},
			want: []string{"top(sum:a{x:1,y:2},10)", "avg:b{*}"},
		},
		{
			name: "trailing comma",
			in:   []string{"a,b,"},
			want: []string{"a", "b"},
		},
		{
			name: "empty string",
			in:   []string{""},
			want: nil,
		},
		{
			name: "already-split inputs pass through unchanged",
			in:   []string{"avg:a{*}", "avg:b{*}"},
			want: []string{"avg:a{*}", "avg:b{*}"},
		},
		{
			name: "single quotes with commas",
			in:   []string{"a:'x,y',b:'z'"},
			want: []string{"a:'x,y'", "b:'z'"},
		},
		{
			name: "whitespace around top-level commas is trimmed",
			in:   []string{"avg:a{*} , avg:b{*}"},
			want: []string{"avg:a{*}", "avg:b{*}"},
		},
		{
			name: "multiple values each split",
			in:   []string{"avg:a{x:1,y:2}", "avg:c{p:3,q:4},avg:d{*}"},
			want: []string{"avg:a{x:1,y:2}", "avg:c{p:3,q:4}", "avg:d{*}"},
		},
		{
			name: "no input",
			in:   nil,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitQueriesTopLevel(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitQueriesTopLevel(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
