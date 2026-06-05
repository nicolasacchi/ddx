// Package redact masks known-sensitive fields and PII before they are written
// to verbose stderr logs. It is intentionally copy-identical across the
// gumlet/otx/ddx/kv CLIs (four separate Go modules, no shared parent module).
// Keep the four copies byte-for-byte in sync when editing.
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

const Marker = "***REDACTED***"

// maxBodyLen caps verbose body output so a huge payload cannot flood logs.
const maxBodyLen = 4096

// sensitiveKeys is matched case-insensitively against JSON object keys.
var sensitiveKeys = map[string]struct{}{
	"secret_access_key": {}, "secretaccesskey": {},
	"access_key_id": {}, "accesskeyid": {},
	"secret": {}, "secretkey": {},
	"clientsecret": {}, "client_secret": {},
	"password": {}, "passwd": {},
	"apikey": {}, "api_key": {},
	"token": {}, "access_token": {}, "refresh_token": {},
	"privatekey": {}, "private_key": {},
	"authorization": {},
}

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// Fallback for non-JSON bodies: "key":"value" with a sensitive key.
	kvRe = regexp.MustCompile(`(?i)"(secret_access_key|access_key_id|secret|client_secret|password|api_key|apikey|access_token|refresh_token|private_key|authorization)"\s*:\s*"[^"]*"`)
)

// Body returns a redacted copy of a request/response body suitable for logging.
// JSON is deep-walked; non-JSON falls back to a regex pass. Output is capped.
func Body(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		walk(v)
		if out, err := json.Marshal(v); err == nil {
			return capLen(string(out))
		}
	}
	return capLen(kvRe.ReplaceAllStringFunc(string(b), func(m string) string {
		i := strings.Index(m, ":")
		return m[:i+1] + `"` + Marker + `"`
	}))
}

// URL masks PII in query-string values (inline emails, e.g. Klaviyo
// filter=equals(email,"x@y.com")). The path is left untouched.
func URL(raw string) string {
	i := strings.IndexByte(raw, '?')
	if i < 0 {
		return raw
	}
	q := emailRe.ReplaceAllString(raw[i+1:], Marker)
	return raw[:i+1] + q
}

func walk(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, ok := sensitiveKeys[strings.ToLower(k)]; ok {
				t[k] = Marker
				continue
			}
			walk(val)
		}
	case []any:
		for _, e := range t {
			walk(e)
		}
	}
}

func capLen(s string) string {
	if len(s) > maxBodyLen {
		return s[:maxBodyLen] + "...<truncated>"
	}
	return s
}

// Token returns a non-revealing preview of a bearer/secret for status output,
// e.g. "eyJhbGci...c2ub". Empty input yields "".
func Token(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 12 {
		return Marker
	}
	return s[:8] + "..." + s[len(s)-4:]
}
