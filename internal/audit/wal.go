package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// WALRecord is a single audit log entry in the WAL file with hash chain fields.
type WALRecord struct {
	Seq          uint64  `json:"seq"`
	Timestamp    string  `json:"ts"`
	AuditID      string  `json:"audit_id"`
	UserID       string  `json:"user_id,omitempty"`
	Role         string  `json:"role,omitempty"`
	Department   string  `json:"department,omitempty"`
	Clearance    string  `json:"clearance,omitempty"`
	AuthMethod   string  `json:"auth_method,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	AppID        string  `json:"app_id,omitempty"`
	Model        string  `json:"model,omitempty"`
	InputHash    string  `json:"input_hash,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputHash   string  `json:"output_hash,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
	Action       string  `json:"action"`
	PolicyName   string  `json:"policy_name,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	Detections   string  `json:"detections,omitempty"`
	RiskScore    float64 `json:"risk_score,omitempty"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
	RequestIP    string  `json:"request_ip,omitempty"`
	Error        string  `json:"error,omitempty"`
	Prev         string  `json:"prev"`
	Hash         string  `json:"hash"`
}

// WALCursor tracks how far records have been flushed to the Control Plane.
type WALCursor struct {
	FlushedSeq  uint64 `json:"flushed_seq"`
	FlushedHash string `json:"flushed_hash"`
	FlushedAt   string `json:"flushed_at"`
}

// WALWriter is a file-based audit logger with hash chain integrity.
// It also keeps an in-memory ring buffer for local queries.
// Designed for managed mode where records are periodically flushed to the CP.
type WALWriter struct {
	mu         sync.Mutex
	dir        string // directory for wal files
	file       *os.File
	nextSeq    uint64
	lastHash   string
	logger     zerolog.Logger

	// In-memory ring buffer for local Query() support
	ring     []Record
	ringSize int
	ringHead int
	ringFull bool

	// Cursor state
	cursor WALCursor

	// Limits
	maxFileSize int64 // max WAL file size in bytes (default 50MB)
}

const (
	walFileName    = "audit_buffer.jsonl"
	cursorFileName = "audit_cursor.json"
	defaultMaxSize = 50 * 1024 * 1024 // 50MB
	hashChainSeed  = "0000000000000000"
	ringBufferSize = 1000 // in-memory query buffer
)

// NewWALWriter creates a new WAL-based audit logger.
// dir is the directory where audit_buffer.jsonl and audit_cursor.json are stored.
func NewWALWriter(dir string, logger zerolog.Logger) (*WALWriter, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}

	w := &WALWriter{
		dir:         dir,
		lastHash:    hashChainSeed,
		logger:      logger.With().Str("component", "audit-wal").Logger(),
		ring:        make([]Record, ringBufferSize),
		ringSize:    ringBufferSize,
		maxFileSize: defaultMaxSize,
	}

	// Load cursor
	if err := w.loadCursor(); err != nil {
		w.logger.Warn().Err(err).Msg("failed to load cursor, starting fresh")
	}

	// Recover state from existing WAL file
	if err := w.recover(); err != nil {
		w.logger.Warn().Err(err).Msg("WAL recovery failed, starting fresh")
		w.nextSeq = w.cursor.FlushedSeq + 1
		w.lastHash = w.cursor.FlushedHash
		if w.lastHash == "" {
			w.lastHash = hashChainSeed
		}
	}

	// Open WAL file for append
	walPath := filepath.Join(dir, walFileName)
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open WAL file: %w", err)
	}
	w.file = f

	return w, nil
}

// Write appends a record to the WAL file with hash chain.
func (w *WALWriter) Write(r Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check file size limit
	if w.maxFileSize > 0 {
		if info, err := w.file.Stat(); err == nil && info.Size() >= w.maxFileSize {
			w.logger.Warn().Int64("size", info.Size()).Msg("WAL file size limit reached, oldest unflushed records will be lost")
			// Compact: keep only unflushed records
			if err := w.compactLocked(); err != nil {
				w.logger.Error().Err(err).Msg("WAL compaction failed")
			}
		}
	}

	wr := WALRecord{
		Seq:          w.nextSeq,
		Timestamp:    r.Timestamp.Format(time.RFC3339Nano),
		AuditID:      r.AuditID,
		UserID:       r.UserID,
		Role:         r.Role,
		Department:   r.Department,
		Clearance:    r.Clearance,
		AuthMethod:   r.AuthMethod,
		SessionID:    r.SessionID,
		AppID:        r.AppID,
		Model:        r.Model,
		InputHash:    r.InputHash,
		InputTokens:  r.InputTokens,
		OutputHash:   r.OutputHash,
		OutputTokens: r.OutputTokens,
		FinishReason: r.FinishReason,
		Action:       r.Action,
		PolicyName:   r.PolicyName,
		Reason:       r.Reason,
		Detections:   r.Detections,
		RiskScore:    r.RiskScore,
		DurationMs:   r.DurationMs,
		RequestIP:    r.RequestIP,
		Error:        r.Error,
		Prev:         w.lastHash,
	}

	wr.Hash = computeHash(wr)

	line, err := json.Marshal(wr)
	if err != nil {
		return fmt.Errorf("marshal WAL record: %w", err)
	}
	line = append(line, '\n')

	if _, err := w.file.Write(line); err != nil {
		return fmt.Errorf("write WAL record: %w", err)
	}

	w.lastHash = wr.Hash
	w.nextSeq++

	// Also store in ring buffer for Query()
	w.ring[w.ringHead] = r
	w.ringHead = (w.ringHead + 1) % w.ringSize
	if w.ringHead == 0 {
		w.ringFull = true
	}

	return nil
}

// Query returns records from the in-memory ring buffer.
func (w *WALWriter) Query(opts QueryOpts) ([]Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	size := w.ringSize
	if !w.ringFull {
		size = w.ringHead
	}

	var results []Record
	for i := 0; i < size; i++ {
		idx := (w.ringHead - 1 - i + w.ringSize) % w.ringSize
		r := w.ring[idx]
		if r.AuditID == "" {
			continue
		}
		if opts.Action != "" && !strings.EqualFold(r.Action, opts.Action) {
			continue
		}
		if opts.UserID != "" && r.UserID != opts.UserID {
			continue
		}
		if opts.SessionID != "" && r.SessionID != opts.SessionID {
			continue
		}
		if !opts.Since.IsZero() && r.Timestamp.Before(opts.Since) {
			continue
		}
		results = append(results, r)
		limit := opts.Limit
		if limit <= 0 {
			limit = 50
		}
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// Count returns the number of records in the ring buffer.
func (w *WALWriter) Count() (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ringFull {
		return w.ringSize, nil
	}
	return w.ringHead, nil
}

// Close syncs and closes the WAL file.
func (w *WALWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		w.file.Sync()
		return w.file.Close()
	}
	return nil
}

// UnflushedRecords returns all WAL records with seq > cursor.FlushedSeq.
// Used by the sync client to push records to the Control Plane.
func (w *WALWriter) UnflushedRecords() ([]WALRecord, error) {
	w.mu.Lock()
	flushedSeq := w.cursor.FlushedSeq
	w.mu.Unlock()

	walPath := filepath.Join(w.dir, walFileName)
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []WALRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024) // max 1MB per line
	for scanner.Scan() {
		var wr WALRecord
		if err := json.Unmarshal(scanner.Bytes(), &wr); err != nil {
			continue // skip corrupted lines
		}
		if wr.Seq > flushedSeq {
			records = append(records, wr)
		}
	}
	return records, scanner.Err()
}

// MarkFlushed updates the cursor to indicate records up to the given seq
// have been accepted by the Control Plane.
func (w *WALWriter) MarkFlushed(seq uint64, hash string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.cursor.FlushedSeq = seq
	w.cursor.FlushedHash = hash
	w.cursor.FlushedAt = time.Now().Format(time.RFC3339)

	return w.saveCursorLocked()
}

// LastSeqAndHash returns the current last seq and hash for the CP push request.
func (w *WALWriter) LastSeqAndHash() (uint64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.nextSeq == 0 {
		return 0, hashChainSeed
	}
	return w.nextSeq - 1, w.lastHash
}

// CursorState returns the current cursor state.
func (w *WALWriter) CursorState() WALCursor {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cursor
}

// Compact removes already-flushed records from the WAL file.
// Called periodically (e.g., once per day) to keep the file small.
func (w *WALWriter) Compact() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.compactLocked()
}

func (w *WALWriter) compactLocked() error {
	walPath := filepath.Join(w.dir, walFileName)
	tmpPath := walPath + ".tmp"

	// Read all unflushed records
	src, err := os.Open(walPath)
	if err != nil {
		return err
	}

	var kept [][]byte
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		var wr WALRecord
		if err := json.Unmarshal(scanner.Bytes(), &wr); err != nil {
			continue
		}
		if wr.Seq > w.cursor.FlushedSeq {
			line := make([]byte, len(scanner.Bytes()))
			copy(line, scanner.Bytes())
			kept = append(kept, line)
		}
	}
	src.Close()

	// Write kept records to temp file
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	for _, line := range kept {
		tmp.Write(line)
		tmp.Write([]byte{'\n'})
	}
	tmp.Sync()
	tmp.Close()

	// Close current file, rename, reopen
	w.file.Close()

	if err := os.Rename(tmpPath, walPath); err != nil {
		// Reopen original
		w.file, _ = os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		return fmt.Errorf("rename WAL: %w", err)
	}

	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("reopen WAL: %w", err)
	}
	w.file = f

	w.logger.Info().Int("kept", len(kept)).Uint64("flushed_seq", w.cursor.FlushedSeq).Msg("WAL compacted")
	return nil
}

// VerifyChain validates the hash chain integrity of the WAL file.
// Returns the number of valid records and the first error encountered.
func (w *WALWriter) VerifyChain() (int, error) {
	walPath := filepath.Join(w.dir, walFileName)
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	var (
		count    int
		prevHash = hashChainSeed
		lastSeq  uint64
	)

	for scanner.Scan() {
		var wr WALRecord
		if err := json.Unmarshal(scanner.Bytes(), &wr); err != nil {
			return count, fmt.Errorf("line %d: invalid JSON: %w", count+1, err)
		}

		// Verify chain linkage
		if count > 0 && wr.Prev != prevHash {
			return count, fmt.Errorf("seq %d: chain broken (prev=%s, expected=%s)", wr.Seq, wr.Prev, prevHash)
		}

		// Verify sequence continuity
		if count > 0 && wr.Seq != lastSeq+1 {
			return count, fmt.Errorf("seq %d: sequence gap (expected %d)", wr.Seq, lastSeq+1)
		}

		// Verify hash
		expected := computeHash(wr)
		if wr.Hash != expected {
			return count, fmt.Errorf("seq %d: hash mismatch", wr.Seq)
		}

		prevHash = wr.Hash
		lastSeq = wr.Seq
		count++
	}

	return count, scanner.Err()
}

// recover reads the existing WAL file to restore nextSeq and lastHash.
func (w *WALWriter) recover() error {
	walPath := filepath.Join(w.dir, walFileName)
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.nextSeq = w.cursor.FlushedSeq + 1
			w.lastHash = w.cursor.FlushedHash
			if w.lastHash == "" {
				w.lastHash = hashChainSeed
			}
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	var lastRecord WALRecord
	found := false
	for scanner.Scan() {
		var wr WALRecord
		if err := json.Unmarshal(scanner.Bytes(), &wr); err != nil {
			continue
		}
		lastRecord = wr
		found = true

		// Also populate ring buffer with record
		r := walRecordToRecord(wr)
		w.ring[w.ringHead] = r
		w.ringHead = (w.ringHead + 1) % w.ringSize
		if w.ringHead == 0 {
			w.ringFull = true
		}
	}

	if found {
		w.nextSeq = lastRecord.Seq + 1
		w.lastHash = lastRecord.Hash
	} else {
		w.nextSeq = w.cursor.FlushedSeq + 1
		w.lastHash = w.cursor.FlushedHash
		if w.lastHash == "" {
			w.lastHash = hashChainSeed
		}
	}

	return scanner.Err()
}

func (w *WALWriter) loadCursor() error {
	cursorPath := filepath.Join(w.dir, cursorFileName)
	data, err := os.ReadFile(cursorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &w.cursor)
}

func (w *WALWriter) saveCursorLocked() error {
	cursorPath := filepath.Join(w.dir, cursorFileName)
	data, err := json.MarshalIndent(w.cursor, "", "  ")
	if err != nil {
		return err
	}

	// Write atomically via temp file
	tmpPath := cursorPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, cursorPath)
}

// computeHash calculates SHA256 of the record's essential fields + prev hash.
func computeHash(wr WALRecord) string {
	// Zero out hash field for computation
	data := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%d|%s|%d|%s|%s|%s|%v|%d|%s|%s",
		wr.Seq, wr.Timestamp, wr.AuditID, wr.UserID, wr.Role, wr.Department,
		wr.SessionID, wr.AppID, wr.Model,
		wr.InputHash, wr.Action, wr.InputTokens,
		wr.OutputHash, wr.OutputTokens, wr.FinishReason,
		wr.PolicyName, wr.Reason, wr.RiskScore, wr.DurationMs,
		wr.RequestIP, wr.Prev,
	)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// walRecordToRecord converts a WALRecord back to an audit.Record.
func walRecordToRecord(wr WALRecord) Record {
	ts, _ := time.Parse(time.RFC3339Nano, wr.Timestamp)
	return Record{
		AuditID:      wr.AuditID,
		Timestamp:    ts,
		UserID:       wr.UserID,
		Role:         wr.Role,
		Department:   wr.Department,
		Clearance:    wr.Clearance,
		AuthMethod:   wr.AuthMethod,
		SessionID:    wr.SessionID,
		AppID:        wr.AppID,
		Model:        wr.Model,
		InputHash:    wr.InputHash,
		InputTokens:  wr.InputTokens,
		OutputHash:   wr.OutputHash,
		OutputTokens: wr.OutputTokens,
		FinishReason: wr.FinishReason,
		Action:       wr.Action,
		PolicyName:   wr.PolicyName,
		Reason:       wr.Reason,
		Detections:   wr.Detections,
		RiskScore:    wr.RiskScore,
		DurationMs:   wr.DurationMs,
		RequestIP:    wr.RequestIP,
		Error:        wr.Error,
	}
}

// Ensure WALWriter implements Logger
var _ Logger = (*WALWriter)(nil)

// ReadAllRecords reads all WAL records from file. Used for verification and testing.
func ReadAllRecords(r io.Reader) ([]WALRecord, error) {
	var records []WALRecord
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		var wr WALRecord
		if err := json.Unmarshal(scanner.Bytes(), &wr); err != nil {
			continue
		}
		records = append(records, wr)
	}
	return records, scanner.Err()
}
