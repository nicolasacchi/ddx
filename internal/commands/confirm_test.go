package commands

import (
	"errors"
	"testing"

	"github.com/nicolasacchi/ddx/internal/client"
)

func TestRequireConfirm(t *testing.T) {
	cases := []struct {
		name    string
		yes     bool
		wantErr bool
	}{
		{"refuses without --yes", false, true},
		{"proceeds with --yes", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := yesFlag
			t.Cleanup(func() { yesFlag = orig })
			yesFlag = tc.yes

			err := requireConfirm("deleting monitor 123")
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected nil with --yes, got %v", err)
				}
				return
			}
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) || apiErr.Kind != "write_locked" {
				t.Fatalf("expected write_locked APIError, got %v", err)
			}
			if apiErr.ExitCode() != 6 {
				t.Fatalf("write_locked must map to exit 6, got %d", apiErr.ExitCode())
			}
		})
	}
}
