package audit

import (
	"testing"
	"time"
)

func TestMemoryLogger_GetByID(t *testing.T) {
	m := NewMemoryLogger(10)

	r1 := Record{AuditID: "audit-001", Timestamp: time.Now(), UserID: "yamada", Action: "block", PolicyName: "block_pii"}
	r2 := Record{AuditID: "audit-002", Timestamp: time.Now(), UserID: "tanaka", Action: "allow"}
	m.Write(r1)
	m.Write(r2)

	// Found
	got, err := m.GetByID("audit-001")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.AuditID != "audit-001" {
		t.Errorf("got AuditID=%q, want audit-001", got.AuditID)
	}
	if got.PolicyName != "block_pii" {
		t.Errorf("got PolicyName=%q, want block_pii", got.PolicyName)
	}

	// Not found
	_, err = m.GetByID("audit-999")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestWALWriter_GetByID(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}
	defer w.Close()

	r1 := Record{AuditID: "audit-w01", Timestamp: time.Now(), UserID: "yamada", Action: "warn", PolicyName: "warn_pii"}
	r2 := Record{AuditID: "audit-w02", Timestamp: time.Now(), UserID: "tanaka", Action: "block"}
	w.Write(r1)
	w.Write(r2)

	// Found
	got, err := w.GetByID("audit-w01")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.AuditID != "audit-w01" {
		t.Errorf("got AuditID=%q, want audit-w01", got.AuditID)
	}

	// Not found
	_, err = w.GetByID("audit-999")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
