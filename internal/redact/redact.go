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

// sensitiveKeys is matched case-insensitively against JSON object keys; the
// value is fully masked.
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

// piiKeys holds object keys whose value is personal data and is masked
// regardless of content — covers e.g. national-format phone numbers that carry
// no "+" prefix and so would slip past phoneRe.
var piiKeys = map[string]struct{}{
	"email": {}, "email_address": {}, "emailaddress": {},
	"phone": {}, "phone_number": {}, "phonenumber": {},
	"mobile": {}, "mobile_number": {},
}

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// E.164-style phone: a leading "+" and 8-15 digits. The "+" requirement
	// keeps it from matching bare IDs, amounts, timestamps, or timezone offsets.
	phoneRe = regexp.MustCompile(`\+[0-9]{8,15}`)
	// Fallback for non-JSON bodies: "key":"value" with a sensitive key
	// (case-insensitive; includes camelCase/concatenated variants).
	kvRe = regexp.MustCompile(`(?i)"(secret_access_key|secretaccesskey|access_key_id|accesskeyid|secret|secretkey|client_secret|clientsecret|password|passwd|api_key|apikey|access_token|refresh_token|private_key|privatekey|authorization)"\s*:\s*"[^"]*"`)
)

// Body returns a redacted copy of a request/response body suitable for logging.
// JSON is deep-walked (sensitive/PII keys masked, embedded email/phone PII in
// any string masked); non-JSON falls back to a regex pass. Output is capped.
func Body(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		v = walk(v)
		if out, err := json.Marshal(v); err == nil {
			return capLen(string(out))
		}
	}
	s := kvRe.ReplaceAllStringFunc(string(b), func(m string) string {
		i := strings.Index(m, ":")
		return m[:i+1] + `"` + Marker + `"`
	})
	return capLen(maskPII(s))
}

// URL masks PII in query-string values (emails and E.164 phones, e.g. Klaviyo
// filter=equals(email,"x@y.com")). The path is left untouched.
func URL(raw string) string {
	i := strings.IndexByte(raw, '?')
	if i < 0 {
		return raw
	}
	return raw[:i+1] + maskPII(raw[i+1:])
}

// maskPII replaces emails and E.164 phone numbers anywhere in s.
func maskPII(s string) string {
	s = emailRe.ReplaceAllString(s, Marker)
	s = phoneRe.ReplaceAllString(s, Marker)
	return s
}

// walk deep-redacts a decoded JSON value in place and returns it: values under a
// sensitive or PII key are fully masked; every other string has embedded
// email/phone PII masked.
func walk(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(k)
			if _, ok := sensitiveKeys[lk]; ok {
				t[k] = Marker
				continue
			}
			if _, ok := piiKeys[lk]; ok {
				t[k] = Marker
				continue
			}
			t[k] = walk(val)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = walk(e)
		}
		return t
	case string:
		return maskPII(t)
	}
	return v
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
