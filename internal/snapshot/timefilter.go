package snapshot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/timeutil"
)

func itemTime(item json.RawMessage, field string) (time.Time, bool) {
	var stamps struct {
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(item, &stamps); err != nil {
		return time.Time{}, false
	}
	raw := stamps.UpdatedAt
	if field == "created" {
		raw = stamps.CreatedAt
	}
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// FilterRecords applies the optional local time window to raw records. It does
// not mutate the input slice's elements and returns records with usable
// timestamps only when a filter is active.
func FilterRecords(records []Record, scope Scope, now time.Time) ([]Record, error) {
	field := strings.ToLower(strings.TrimSpace(scope.DateField))
	if field == "" {
		field = "updated"
	}
	if field != "created" && field != "updated" {
		return nil, fmt.Errorf("invalid date field %q: use created or updated", scope.DateField)
	}

	// Trim so a whitespace-only value means "no filter", like cmd/report.
	sinceRaw := strings.TrimSpace(scope.Since)
	untilRaw := strings.TrimSpace(scope.Until)
	if sinceRaw == "" && untilRaw == "" {
		return records, nil
	}

	var since, until time.Time
	var err error
	if sinceRaw != "" {
		since, err = timeutil.ParseTime(sinceRaw, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --since: %w", err)
		}
	}
	if untilRaw != "" {
		until, err = timeutil.ParseTime(untilRaw, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --until: %w", err)
		}
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return nil, fmt.Errorf("invalid snapshot window: --since is after --until")
	}

	kept := make([]Record, 0, len(records))
	for _, record := range records {
		t, ok := itemTime(record.Item, field)
		if !ok {
			continue
		}
		if !since.IsZero() && t.Before(since) {
			continue
		}
		if !until.IsZero() && t.After(until) {
			continue
		}
		kept = append(kept, record)
	}
	return kept, nil
}
