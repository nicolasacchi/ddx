package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parse converts a time string to a Unix timestamp in seconds.
// Supports:
//   - Relative: "1h", "5m", "24h", "7d", "30d", "now-1h"
//   - Absolute: RFC3339 ("2026-03-19T09:00:00Z")
//   - Unix: epoch seconds or milliseconds
//   - Special: "now"
func Parse(s string) (int64, error) {
	if s == "" || s == "now" {
		return time.Now().Unix(), nil
	}

	// "now-1h" style
	if strings.HasPrefix(s, "now-") {
		dur, err := parseDuration(s[4:])
		if err != nil {
			return 0, fmt.Errorf("parse relative time %q: %w", s, err)
		}
		return time.Now().Add(-dur).Unix(), nil
	}

	// Pure relative like "1h", "7d" (treated as now-X)
	if dur, err := parseDuration(s); err == nil {
		return time.Now().Add(-dur).Unix(), nil
	}

	// RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}

	// Unix timestamp
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		// If > 1e12, it's milliseconds
		if n > 1e12 {
			return n / 1000, nil
		}
		return n, nil
	}

	return 0, fmt.Errorf("cannot parse time %q: use relative (1h, 7d), RFC3339, or Unix timestamp", s)
}

// ParseMillis is like Parse but returns milliseconds.
func ParseMillis(s string) (int64, error) {
	secs, err := Parse(s)
	if err != nil {
		return 0, err
	}
	return secs * 1000, nil
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Find where the number ends and the unit begins
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("no number in duration %q", s)
	}

	numStr := s[:i]
	unit := strings.TrimSpace(s[i:])

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in %q: %w", s, err)
	}

	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Duration(num * float64(time.Second)), nil
	case "m", "min", "minute", "minutes":
		return time.Duration(num * float64(time.Minute)), nil
	case "h", "hr", "hour", "hours":
		return time.Duration(num * float64(time.Hour)), nil
	case "d", "day", "days":
		return time.Duration(num * 24 * float64(time.Hour)), nil
	case "w", "week", "weeks":
		return time.Duration(num * 7 * 24 * float64(time.Hour)), nil
	case "":
		// No unit — try Go's time.ParseDuration as fallback
		return time.ParseDuration(s)
	default:
		return 0, fmt.Errorf("unknown time unit %q in %q", unit, s)
	}
}
