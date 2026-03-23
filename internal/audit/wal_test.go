package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func testLogger() zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
}

func TestWALWriter_WriteAndQuery(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}
	defer w.Close()

	records := []Record{
		{AuditID: "a1", Timestamp: time.Now().Add(-2 * time.Hour), UserID: "yamada", Action: "allow"},
		{AuditID: "a2", Timestamp: time.Now().Add(-1 * time.Hour), UserID: "tanaka", Action: "block", PolicyName: "block_injection"},
		{AuditID: "a3", Timestamp: time.Now(), UserID: "yamada", Action: "warn"},
	}

	for _, r := range records {
		if err := w.Write(r); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	count, err := w.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}

	results, err := w.Query(QueryOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Query all: got %d, want 3", len(results))
	}

	results, err = w.Query(QueryOpts{Action: "block", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Query block: got %d, want 1", len(results))
	}

	results, err = w.Query(QueryOpts{UserID: "yamada", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Query yamada: got %d, want 2", len(results))
	}
}

func TestWALWriter_HashChainIntegrity(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}

	for i := 0; i < 10; i++ {
		r := Record{
			AuditID:   fmt.Sprintf("a%d", i),
			Timestamp: time.Now(),
			UserID:    "test",
			Action:    "allow",
		}
		if err := w.Write(r); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	w.Close()

	w2, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter (reopen): %v", err)
	}
	defer w2.Close()

	valid, verr := w2.VerifyChain()
	if verr != nil {
		t.Fatalf("VerifyChain error: %v", verr)
	}
	if valid != 10 {
		t.Errorf("VerifyChain: valid = %d, want 10", valid)
	}
}

func TestWALWriter_TamperDetection(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}

	for i := 0; i < 5; i++ {
		r := Record{
			AuditID:   fmt.Sprintf("a%d", i),
			Timestamp: time.Now(),
			Action:    "allow",
		}
		w.Write(r)
	}
	w.Close()

	// Tamper with the WAL file
	walPath := filepath.Join(dir, walFileName)
	data, _ := os.ReadFile(walPath)
	modified := false
	for i := len(data) / 2; i < len(data); i++ {
		if data[i] == 'a' {
			data[i] = 'b'
			modified = true
			break
		}
	}
	if !modified {
		t.Skip("could not find a character to tamper")
	}
	os.WriteFile(walPath, data, 0600)

	w2, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter (tampered): %v", err)
	}
	defer w2.Close()

	_, verr := w2.VerifyChain()
	if verr == nil {
		t.Error("VerifyChain should have detected tampering")
	}
}

func TestWALWriter_CursorAndFlush(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}

	// Write 5 records (seq starts at 1: seq 1,2,3,4,5)
	for i := 0; i < 5; i++ {
		r := Record{
			AuditID:   fmt.Sprintf("a%d", i),
			Timestamp: time.Now(),
			Action:    "allow",
		}
		w.Write(r)
	}

	// All 5 should be unflushed (cursor.FlushedSeq defaults to 0)
	unflushed, err := w.UnflushedRecords()
	if err != nil {
		t.Fatalf("UnflushedRecords: %v", err)
	}
	if len(unflushed) != 5 {
		t.Errorf("UnflushedRecords: got %d, want 5", len(unflushed))
	}

	// Mark seq 1,2,3 as flushed
	if err := w.MarkFlushed(3, unflushed[2].Hash); err != nil {
		t.Fatalf("MarkFlushed: %v", err)
	}

	// Unflushed should now be 2 (seq 4,5)
	unflushed, err = w.UnflushedRecords()
	if err != nil {
		t.Fatalf("UnflushedRecords: %v", err)
	}
	if len(unflushed) != 2 {
		t.Errorf("UnflushedRecords after flush: got %d, want 2", len(unflushed))
	}

	// Verify cursor persists across restart
	w.Close()
	w2, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter (reopen): %v", err)
	}
	defer w2.Close()

	cursor := w2.CursorState()
	if cursor.FlushedSeq != 3 {
		t.Errorf("Cursor FlushedSeq = %d, want 3", cursor.FlushedSeq)
	}

	unflushed, err = w2.UnflushedRecords()
	if err != nil {
		t.Fatalf("UnflushedRecords (reopened): %v", err)
	}
	if len(unflushed) != 2 {
		t.Errorf("UnflushedRecords (reopened): got %d, want 2", len(unflushed))
	}
}

func TestWALWriter_Compaction(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}

	// Write 10 records (seq 1-10)
	for i := 0; i < 10; i++ {
		r := Record{
			AuditID:   fmt.Sprintf("a%d", i),
			Timestamp: time.Now(),
			Action:    "allow",
		}
		w.Write(r)
	}

	// Mark seq 1-7 as flushed
	unflushed, _ := w.UnflushedRecords()
	w.MarkFlushed(7, unflushed[6].Hash)

	if err := w.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// After compaction, only 3 records should remain (seq 8,9,10)
	unflushed, err = w.UnflushedRecords()
	if err != nil {
		t.Fatalf("UnflushedRecords after compact: %v", err)
	}
	if len(unflushed) != 3 {
		t.Errorf("UnflushedRecords after compact: got %d, want 3", len(unflushed))
	}

	// Should still be able to write new records
	if err := w.Write(Record{AuditID: "a10", Timestamp: time.Now(), Action: "block"}); err != nil {
		t.Fatalf("Write after compact: %v", err)
	}

	unflushed, _ = w.UnflushedRecords()
	if len(unflushed) != 4 {
		t.Errorf("UnflushedRecords after compact+write: got %d, want 4", len(unflushed))
	}

	w.Close()
}

func TestWALWriter_RecoveryAfterCrash(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		w.Write(Record{
			AuditID:   fmt.Sprintf("a%d", i),
			Timestamp: time.Now(),
			Action:    "allow",
		})
	}
	// Don't close - simulate crash

	w2, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter (recovery): %v", err)
	}
	defer w2.Close()

	// seq 1-5 were written, last seq = 5
	lastSeq, _ := w2.LastSeqAndHash()
	if lastSeq != 5 {
		t.Errorf("LastSeq after recovery = %d, want 5", lastSeq)
	}

	w2.Write(Record{AuditID: "a5", Timestamp: time.Now(), Action: "block"})

	valid, verr := w2.VerifyChain()
	if verr != nil {
		t.Fatalf("VerifyChain after recovery: %v", verr)
	}
	if valid != 6 {
		t.Errorf("VerifyChain after recovery: valid = %d, want 6", valid)
	}
}

func TestWALWriter_SeqStartsAt1(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWALWriter(dir, testLogger())
	if err != nil {
		t.Fatalf("NewWALWriter: %v", err)
	}
	defer w.Close()

	for i := 0; i < 3; i++ {
		w.Write(Record{AuditID: fmt.Sprintf("a%d", i), Timestamp: time.Now(), Action: "allow"})
	}

	// Seq starts at 1 (0 is the "nothing flushed" sentinel)
	records, _ := w.UnflushedRecords()
	for i, r := range records {
		expectedSeq := uint64(i + 1) // 1-based
		if r.Seq != expectedSeq {
			t.Errorf("record %d: seq = %d, want %d", i, r.Seq, expectedSeq)
		}
	}
}
