package output

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestHelpersIsEmptyFilterResult(t *testing.T) {
	cases := []struct {
		name string
		json string
		path string
		want bool
	}{
		{
			name: "existing field is not empty",
			json: `{"data":[{"id":"1"}]}`,
			path: "data.0.id",
			want: false,
		},
		{
			name: "projection is not empty",
			json: `{"data":[{"a":1,"b":2}]}`,
			path: "data.0.{a,b}",
			want: false,
		},
		{
			name: "legitimately empty array is not an empty result",
			json: `{"data":[]}`,
			path: "data",
			want: false,
		},
		{
			name: "nonexistent path is empty",
			json: `{"data":[{"id":"1"}]}`,
			path: "nope.does.not.exist",
			want: true,
		},
		{
			name: "typo'd path is empty",
			json: `{"data":[{"id":"1"}]}`,
			path: "dat.0.id",
			want: true,
		},
		{
			name: "zero-value result is empty",
			json: "",
			path: "",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result gjson.Result
			if tc.json != "" {
				result = gjson.Get(tc.json, tc.path)
			}
			if got := helpersIsEmptyFilterResult(result); got != tc.want {
				t.Fatalf("helpersIsEmptyFilterResult(%q on %q) = %v, want %v", tc.path, tc.json, got, tc.want)
			}
		})
	}

	// Directly exercise the "exists but Raw is empty" edge case without
	// relying on gjson producing it naturally — this is the branch that
	// distinguishes helpersIsEmptyFilterResult from a plain !Exists() check.
	t.Run("exists but raw is empty is treated as empty", func(t *testing.T) {
		r := gjson.Result{Type: gjson.String, Raw: ""}
		if !helpersIsEmptyFilterResult(r) {
			t.Fatalf("expected empty result for Type=String with empty Raw")
		}
	})
}

func TestHelpersHasContent(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   \n\t", false},
		{"bare null", "null", false},
		{"empty array", "[]", false},
		{"empty object", "{}", false},
		{"array with items", `[{"id":1}]`, true},
		{"object with fields", `{"data":[{"id":1}]}`, true},
		{"padded empty object", "  {}  ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := helpersHasContent([]byte(tc.data)); got != tc.want {
				t.Fatalf("helpersHasContent(%q) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}
