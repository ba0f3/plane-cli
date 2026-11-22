package snapshot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recordWithTimes(t *testing.T, created, updated string) Record {
	t.Helper()
	item, err := json.Marshal(map[string]string{
		"id": "i-1", "created_at": created, "updated_at": updated,
	})
	require.NoError(t, err)
	return Record{Workspace: "ws", ProjectID: "p1", ProjectIdentifier: "GAME", Item: item}
}

func TestFilterRecordsWithoutRangeKeepsEverything(t *testing.T) {
	records := []Record{recordWithTimes(t, "", "")}
	got, err := FilterRecords(records, Scope{}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, records, got)
}

func TestFilterRecordsUsesCreatedFieldInclusively(t *testing.T) {
	records := []Record{
		recordWithTimes(t, "2026-11-01T00:00:00Z", "2026-11-20T00:00:00Z"),
		recordWithTimes(t, "2026-10-31T23:59:59Z", "2026-11-21T00:00:00Z"),
		recordWithTimes(t, "2026-11-30T00:00:00Z", "2026-11-22T00:00:00Z"),
	}
	scope := Scope{DateField: "created", Since: "2026-11-01", Until: "2026-11-30"}

	got, err := FilterRecords(records, scope, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "2026-11-01T00:00:00Z", timeFromRecord(t, got[0], "created"))
}

func TestFilterRecordsUsesUpdatedFieldByDefault(t *testing.T) {
	records := []Record{
		recordWithTimes(t, "2026-01-01T00:00:00Z", "2026-11-10T00:00:00Z"),
		recordWithTimes(t, "2026-11-01T00:00:00Z", "2026-10-01T00:00:00Z"),
	}
	got, err := FilterRecords(records, Scope{Since: "2026-11-01"}, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestFilterRecordsSkipsItemsWithoutUsableTimestamp(t *testing.T) {
	item, err := json.Marshal(map[string]string{"id": "i-2"})
	require.NoError(t, err)
	records := []Record{{Workspace: "ws", ProjectID: "p1", Item: item}}

	got, err := FilterRecords(records, Scope{Since: "2026-11-01"}, time.Now())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFilterRecordsRejectsInvalidInputs(t *testing.T) {
	_, err := FilterRecords(nil, Scope{DateField: "completed"}, time.Now())
	assert.ErrorContains(t, err, "date field")

	_, err = FilterRecords(nil, Scope{Since: "bad"}, time.Now())
	assert.ErrorContains(t, err, "since")
}

func timeFromRecord(t *testing.T, record Record, field string) string {
	t.Helper()
	var item map[string]string
	require.NoError(t, json.Unmarshal(record.Item, &item))
	return item[field+"_at"]
}
