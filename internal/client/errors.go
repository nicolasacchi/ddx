package client

import (
	"fmt"

	"github.com/nicolasacchi/clicore/cierrors"
)

type APIError struct {
	StatusCode int
	Code       string
	Title      string
	Detail     string
	Hint       string

	// Kind classifies client-side guard errors raised before a request
	// (StatusCode stays 0). Currently: "write_locked" (confirm gate).
	Kind string
}

func (e *APIError) Error() string {
	// Client-side guard errors (no HTTP status) render from Detail/Hint.
	if e.Kind != "" && e.StatusCode == 0 {
		msg := e.Detail
		if msg == "" {
			msg = e.Kind
		}
		if e.Hint != "" {
			return fmt.Sprintf("%s — %s", msg, e.Hint)
		}
		return msg
	}
	if e.Detail != "" {
		return fmt.Sprintf("%d: %s", e.StatusCode, e.Detail)
	}
	if e.Title != "" {
		return fmt.Sprintf("%d: %s", e.StatusCode, e.Title)
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

// ExitCode delegates to the fleet-canonical table (auth=2, validation=3,
// not_found=4, rate_limited=5, write_locked=6, async_timeout=7, else 1).
func (e *APIError) ExitCode() int {
	return cierrors.ExitCodeFor(e.StatusCode, e.Kind)
}

func hintForError(statusCode int) string {
	switch statusCode {
	case 401:
		return "check your API key: --api-key flag, DD_API_KEY env, or 'ddx config list'"
	case 403:
		return "your App key may lack the required scopes — check DD_APP_KEY"
	case 429:
		return "rate limited — reduce request frequency or wait"
	case 404:
		return "resource not found — check the ID or query"
	}
	return ""
}
