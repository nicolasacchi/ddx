package client

import "fmt"

type APIError struct {
	StatusCode int
	Code       string
	Title      string
	Detail     string
	Hint       string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%d: %s", e.StatusCode, e.Detail)
	}
	if e.Title != "" {
		return fmt.Sprintf("%d: %s", e.StatusCode, e.Title)
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

func (e *APIError) ExitCode() int {
	switch {
	case e.StatusCode == 401 || e.StatusCode == 403:
		return 2
	case e.StatusCode == 404:
		return 4
	default:
		return 1
	}
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
