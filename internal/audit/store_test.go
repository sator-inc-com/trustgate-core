//go:build controlplane

package audit

import (
	"os"
	"testing"
	"time"
)

func TestStore_WriteAndQuery(t *testing.T) {
	path := t.TempDir() + "/test_audit.db"
	defer os.Remove(path)

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Write records
	records := []Record{
		{AuditID: "a1", Timestamp: time.Now().Add(-2 * time.Hour), UserID: "yamada", Action: "allow", DurationMs: 3},
		{AuditID: "a2", Timestamp: time.Now().Add(-1 * time.Hour), UserID: "tanaka", Action: "block", PolicyName: "block_injection", DurationMs: 2},
		{AuditID: "a3", Timestamp: time.Now(), UserID: "yamada", Action: "mask", PolicyName: "mask_pii", DurationMs: 1},
	}

	for _, r := range records {
		if err := store.Write(r); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// Count
	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}

	// Query all
	results, err := store.Query(QueryOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Query all: got %d, want 3", len(results))
	}

	// Query by action
	results, err = store.Query(QueryOpts{Action: "block", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Query block: got %d, want 1", len(results))
	}
	if results[0].UserID != "tanaka" {
		t.Errorf("Query block: user = %s, want tanaka", results[0].UserID)
	}

	// Query by user
	results, err = store.Query(QueryOpts{UserID: "yamada", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Query yamada: got %d, want 2", len(results))
	}

	// Query by since
	results, err = store.Query(QueryOpts{Since: time.Now().Add(-90 * time.Minute), Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Query since 90m: got %d, want 2", len(results))
	}
}

func TestHashText(t *testing.T) {
	h1 := HashText("hello")
	h2 := HashText("hello")
	h3 := HashText("world")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) < 10 {
		t.Errorf("hash too short: %s", h1)
	}
}
