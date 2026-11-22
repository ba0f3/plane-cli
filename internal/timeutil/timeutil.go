package timeutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTime accepts Nd days, a Go duration, RFC3339, or a date-only value in
// now.Location(). It is shared by report and snapshot so their windows cannot
// drift apart.
func ParseTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return now.Add(-duration), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, now.Location()); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q", value)
}
