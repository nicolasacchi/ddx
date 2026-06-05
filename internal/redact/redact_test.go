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
	out := Body([]byte(`form: "client_secret":"hunter2" trailing`))
	if strings.Contains(out, "hunter2") {
		t.Fatalf("secret leaked via non-JSON path: %s", out)
	}
}

// TestBody_RedactsEmailAndPhonePII covers the PII path: a GDPR/profile body
// carries email + phone in VALUES (not credential keys); both must be masked.
func TestBody_RedactsEmailAndPhonePII(t *testing.T) {
	in := []byte(`{"data":{"attributes":{"email":"jane.doe@example.com","phone_number":"+393331234567","first_name":"Jane"}}}`)
	out := Body(in)
	for _, leak := range []string{"jane.doe@example.com", "+393331234567"} {
		if strings.Contains(out, leak) {
			t.Fatalf("PII %q leaked in verbose body: %s", leak, out)
		}
	}
	if !strings.Contains(out, Marker) {
		t.Fatalf("expected redaction marker, got: %s", out)
	}
}

// TestBody_MasksEmbeddedPII catches PII embedded in a free-text value under a
// non-PII key (e.g. a note), via the email/phone regex catch-all.
func TestBody_MasksEmbeddedPII(t *testing.T) {
	out := Body([]byte(`{"note":"reach jane.doe@example.com or +12025550123"}`))
	if strings.Contains(out, "jane.doe@example.com") || strings.Contains(out, "+12025550123") {
		t.Fatalf("embedded PII leaked: %s", out)
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
