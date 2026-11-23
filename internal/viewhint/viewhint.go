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
	// maxEvents bounds the cache file. Once the file holds this many lines it
	// is rewritten with only the events still inside Window, keeping room for
	// one more append, so the file never holds more than maxEvents lines.
	maxEvents = 500
)

// CacheDir resolves the base cache directory. It is exported as a test seam:
// tests may override it to redirect the cache file into a temporary directory
// and must restore the original value when they finish.
var CacheDir = os.UserCacheDir

type event struct {
	Key   string `json:"key"`
	Event string `json:"event"`
	At    string `json:"at"`
}

func eventsPath() (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plane-cli", "view-hints.jsonl"), nil
}

// Check records a view for the key and reports whether the periodic hint should
// be emitted. The hint fires on the Threshold-th view inside Window and at
// most once per 5-minute window: once a warning has been recorded for the key
// inside the window, further hints are suppressed until that window passes.
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

	// Bound the cache file: once it holds maxEvents lines, rewrite it with only
	// the events still inside Window. Pruning is best-effort; a failure must
	// never break issue view. The rewrite leaves room for one more event so
	// the warn append below can never push the file past maxEvents lines.
	pruneEvents(filePath, events, now)

	cutoff := now.Add(-Window)
	var views int
	var warned bool
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
			views++
		case "warn":
			warned = true
		}
	}
	if views < Threshold || warned {
		return false, nil
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

// pruneEvents keeps the cache file bounded. It only rewrites the file when it
// holds maxEvents lines or more. The rewritten file keeps the events still
// inside Window (warn events are always kept so suppression survives the
// rewrite) and, if that is still too many, the most recent ones up to
// maxEvents-1. The extra slot leaves room for the warn event that Check may
// append right after the prune, so the file never exceeds maxEvents lines.
// Failures are ignored: pruning is best-effort and must never break
// issue view.
func pruneEvents(filePath string, events []event, now time.Time) {
	if len(events) < maxEvents {
		return
	}
	cutoff := now.Add(-Window)
	kept := make([]event, 0, len(events))
	for _, item := range events {
		at, err := time.Parse(time.RFC3339Nano, item.At)
		if err != nil {
			continue
		}
		if !at.After(cutoff) {
			continue
		}
		kept = append(kept, item)
	}
	// The file is append-only in timestamp order, so the newest events are at
	// the end. Keep the tail up to the budget and re-attach any warn events
	// that fell outside it, so the once-per-window suppression is preserved
	// without letting the file grow past maxEvents.
	budget := maxEvents - 1
	if len(kept) > budget {
		tail := kept[len(kept)-budget:]
		warns := make([]event, 0, len(kept))
		for _, item := range kept[:len(kept)-budget] {
			if item.Event == "warn" {
				warns = append(warns, item)
			}
		}
		// Drop the oldest warn events if they do not fit next to the tail.
		if len(warns)+len(tail) > budget {
			warns = warns[len(warns)+len(tail)-budget:]
		}
		kept = append(warns, tail...)
	}
	if err := rewriteEvents(filePath, kept); err != nil {
		return
	}
}

// rewriteEvents replaces the cache file with the given events using a
// temporary file in the same directory followed by a rename, so a reader
// never observes a partially written file.
func rewriteEvents(filePath string, events []event) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".view-hints-*.jsonl")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	encoder := json.NewEncoder(tmp)
	for _, item := range events {
		if err := encoder.Encode(item); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filePath)
}
