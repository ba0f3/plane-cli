package viewhint

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Threshold is the number of issue view calls within Window that triggers
	// the hint for a workspace/project key.
	Threshold = 3
	// Window is the sliding window used to count the views of a key.
	Window = 5 * time.Minute
	// Warning is the fixed hint written to stderr when the hint fires.
	Warning = "plane-cli: repeated issue view calls detected (3 in 5m); use plane-cli snapshot to collect all items in one invocation instead of viewing each task."
)

var cacheDir = os.UserCacheDir

type event struct {
	Key   string `json:"key"`
	Event string `json:"event"`
	At    string `json:"at"`
}

func eventsPath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plane-cli", "view-hints.jsonl"), nil
}

// Check records a view for the key and reports whether the periodic hint should
// be emitted. The hint fires on the Threshold-th view inside Window and at
// most once per burst of views: a recorded warning suppresses the hint only
// while an older view of the same burst is still inside the window.
func Check(workspace, project string, now time.Time) (bool, error) {
	key := strings.TrimSpace(workspace) + "/" + strings.TrimSpace(project)
	filePath, err := eventsPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if err := appendEvent(filePath, event{Key: key, Event: "view", At: stamp}); err != nil {
		return false, err
	}

	events, err := readEvents(filePath)
	if err != nil {
		return false, err
	}
	cutoff := now.Add(-Window)
	var views, warns []time.Time
	for _, item := range events {
		if item.Key != key {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, item.At)
		if err != nil || !at.After(cutoff) {
			continue
		}
		switch item.Event {
		case "view":
			views = append(views, at)
		case "warn":
			warns = append(warns, at)
		}
	}
	if len(views) < Threshold {
		return false, nil
	}
	// An earlier warning only blocks the hint while an older view of the same
	// burst is still inside the window; once that view expires the burst is
	// over and the hint may fire again.
	for _, warnedAt := range warns {
		for _, viewedAt := range views {
			if viewedAt.Before(warnedAt) {
				return false, nil
			}
		}
	}
	if err := appendEvent(filePath, event{Key: key, Event: "warn", At: stamp}); err != nil {
		return false, err
	}
	return true, nil
}

func appendEvent(filePath string, item event) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = fmt.Fprintln(file, string(data))
	return err
}

func readEvents(filePath string) ([]event, error) {
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var events []event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item event
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			events = append(events, item)
		}
	}
	return events, scanner.Err()
}
