package redact

import (
	"strings"
	"testing"
)

func TestBody_RedactsKnownSecrets(t *testing.T) {
	in := []byte(`{"config":{"bucket":"b","access_key_id":"AKIAEXAMPLE","secret_access_key":"SUPERSECRET"},"name":"src"}`)
	out := Body(in)
	for _, leak := range []string{"AKIAEXAMPLE", "SUPERSECRET"} {
		if strings.Contains(out, leak) {
			t.Fatalf("secret %q leaked in verbose body: %s", leak, out)
		}
	}
	if !strings.Contains(out, Marker) {
		t.Fatalf("expected redaction marker, got: %s", out)
	}
	if !strings.Contains(out, `"bucket":"b"`) {
		t.Fatalf("non-sensitive field was dropped: %s", out)
	}
}

func TestBody_NonJSONFallback(t *testing.T) {
	out := Body([]byte(`form: "password":"hunter2" trailing`))
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked via non-JSON path: %s", out)
	}
}

func TestURL_MasksEmailPII(t *testing.T) {
	in := `https://a.klaviyo.com/api/profiles/?filter=equals(email,"jane.doe@example.com")`
	out := URL(in)
	if strings.Contains(out, "jane.doe@example.com") {
		t.Fatalf("email PII leaked in verbose URL: %s", out)
	}
	if !strings.HasPrefix(out, "https://a.klaviyo.com/api/profiles/?") {
		t.Fatalf("path was mangled: %s", out)
	}
}

func TestToken_Preview(t *testing.T) {
	if got := Token("eyJhbG12345678c2ub"); strings.Contains(got, "12345678") {
		t.Fatalf("token middle leaked: %s", got)
	}
	if Token("") != "" {
		t.Fatal("empty token should yield empty preview")
	}
}
