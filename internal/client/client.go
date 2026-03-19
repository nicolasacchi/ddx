package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	MaxRetries = 3
	Timeout    = 30 * time.Second
)

type Client struct {
	http       *http.Client
	apiKey     string
	appKey     string
	baseURL    string
	verbose    bool
	maxRetries int
}

// New creates a Datadog API client. The site parameter determines the base URL
// (e.g., "datadoghq.eu" → "https://api.datadoghq.eu").
func New(apiKey, appKey, site string, verbose bool) *Client {
	baseURL := siteToBaseURL(site)
	return &Client{
		http:       &http.Client{Timeout: Timeout},
		apiKey:     apiKey,
		appKey:     appKey,
		baseURL:    baseURL,
		verbose:    verbose,
		maxRetries: MaxRetries,
	}
}

func siteToBaseURL(site string) string {
	if site == "" {
		site = "datadoghq.eu"
	}
	// "datadoghq.eu" → "https://api.datadoghq.eu"
	// "datadoghq.com" → "https://api.datadoghq.com"
	// "us3.datadoghq.com" → "https://api.us3.datadoghq.com"
	if strings.Contains(site, "datadoghq") {
		return "https://api." + site
	}
	return "https://" + site
}

// Get performs a GET request and returns the response body as raw JSON.
func (c *Client) Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	u := c.buildURL(path, params)
	return c.doRequest(ctx, http.MethodGet, u, nil)
}

// Post performs a POST request with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	u := c.buildURL(path, nil)
	return c.doJSON(ctx, http.MethodPost, u, body)
}

// Put performs a PUT request with a JSON body.
func (c *Client) Put(ctx context.Context, path string, body any) (json.RawMessage, error) {
	u := c.buildURL(path, nil)
	return c.doJSON(ctx, http.MethodPut, u, body)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) error {
	u := c.buildURL(path, nil)
	_, err := c.doRequest(ctx, http.MethodDelete, u, nil)
	return err
}

func (c *Client) buildURL(path string, params url.Values) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, body any) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return c.doRequest(ctx, method, rawURL, &buf)
}

func (c *Client) doRequest(ctx context.Context, method, rawURL string, body io.Reader) (json.RawMessage, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "> %s %s\n", method, rawURL)
		if len(bodyBytes) > 0 {
			fmt.Fprintf(os.Stderr, "> Body: %s\n", string(bodyBytes))
		}
	}

	start := time.Now()
	resp, err := c.doWithRetry(ctx, method, rawURL, bodyBytes)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "< %d %s (%s, %s)\n", resp.StatusCode, http.StatusText(resp.StatusCode), elapsed.Round(time.Millisecond), humanBytes(len(respBody)))
	}

	if resp.StatusCode == http.StatusNoContent {
		return json.RawMessage("{}"), nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if c.verbose {
			fmt.Fprintf(os.Stderr, "< Body: %s\n", string(respBody))
		}
		return nil, c.parseError(respBody, resp.StatusCode)
	}

	if len(respBody) == 0 {
		return json.RawMessage("{}"), nil
	}

	return json.RawMessage(respBody), nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)
	req.Header.Set("Accept", "application/json")
	if req.Method == http.MethodPost || req.Method == http.MethodPatch || req.Method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
}

func (c *Client) doWithRetry(ctx context.Context, method, rawURL string, bodyBytes []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt == c.maxRetries {
				return nil, lastErr
			}
			delay := retryDelay(attempt, "")
			if c.verbose {
				fmt.Fprintf(os.Stderr, "! request error, retrying in %s (attempt %d/%d)\n", delay, attempt+1, c.maxRetries)
			}
			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == c.maxRetries {
				return nil, &APIError{
					StatusCode: resp.StatusCode,
					Title:      http.StatusText(resp.StatusCode),
					Detail:     fmt.Sprintf("failed after %d retries", c.maxRetries),
				}
			}
			delay := retryDelay(attempt, resp.Header.Get("Retry-After"))
			if c.verbose {
				fmt.Fprintf(os.Stderr, "! %d %s, retrying in %s (attempt %d/%d)\n",
					resp.StatusCode, http.StatusText(resp.StatusCode), delay, attempt+1, c.maxRetries)
			}
			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		return resp, nil
	}
	return nil, lastErr
}

func (c *Client) parseError(body []byte, statusCode int) *APIError {
	apiErr := &APIError{StatusCode: statusCode}

	// Datadog returns errors in multiple formats
	var errResp map[string]any
	if json.Unmarshal(body, &errResp) == nil {
		// v1: {"errors": ["message"]}
		if errs, ok := errResp["errors"].([]any); ok && len(errs) > 0 {
			if msg, ok := errs[0].(string); ok {
				apiErr.Detail = msg
			}
		}
		// v2: {"errors": [{"detail": "..."}]}
		if errs, ok := errResp["errors"].([]any); ok && len(errs) > 0 {
			if obj, ok := errs[0].(map[string]any); ok {
				if d, ok := obj["detail"].(string); ok {
					apiErr.Detail = d
				}
			}
		}
		// {"message": "..."}
		if msg, ok := errResp["message"].(string); ok && apiErr.Detail == "" {
			apiErr.Detail = msg
		}
	}

	if apiErr.Detail == "" {
		switch statusCode {
		case 401:
			apiErr.Detail = "authentication failed — check DD_API_KEY and DD_APP_KEY"
		case 403:
			apiErr.Detail = "forbidden — your App key may lack required scopes"
		case 404:
			apiErr.Detail = "resource not found"
		default:
			apiErr.Detail = http.StatusText(statusCode)
		}
	}

	apiErr.Hint = hintForError(statusCode)
	return apiErr
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	base := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(rand.IntN(500)) * time.Millisecond
	return base + jitter
}

func humanBytes(b int) string {
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	kb := float64(b) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1fKB", kb)
	}
	return fmt.Sprintf("%.1fMB", kb/1024)
}
