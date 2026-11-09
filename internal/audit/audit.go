// Package audit records who performed which mutating operation
// (replay, purge, publish) against which queue, with which filters and
// how many messages were affected. The store keeps a bounded in-memory
// ring for the UI and optionally appends every entry to a JSONL file
// for a durable trail across restarts.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Operation names used in Entry.Operation.
const (
	OpReplay  = "replay"
	OpPurge   = "purge"
	OpPublish = "publish"
)

// Job statuses mirrored from the replay engine.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusFailed    = "failed"
)

// Filter is one replay filter as seen by the audit log. It mirrors the
// replay engine filter shape without importing it, so the audit package
// stays dependency-free.
type Filter struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Header string `json:"header,omitempty"`
}

// Entry is one audited operation. It is created when the operation
// starts and, for long-running replays, updated once when it ends.
// Semantics of the count fields per operation:
//
//   - purge:   Messages is the queue depth observed right before the purge.
//   - publish: Messages is always 1 (or 0 when the count is unknown).
//   - replay:  Messages is the processed total, plus Succeeded/Skipped/Failed.
type Entry struct {
	ID        string     `json:"id"`
	Time      time.Time  `json:"time"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	User      string     `json:"user"`
	Remote    string     `json:"remote,omitempty"`
	Operation string     `json:"operation"`
	Conn      string     `json:"conn,omitempty"`
	VHost     string     `json:"vhost,omitempty"`
	Queue     string     `json:"queue,omitempty"`
	Target    string     `json:"target,omitempty"`
	Filters   []Filter   `json:"filters,omitempty"`
	DryRun    bool       `json:"dry_run,omitempty"`
	Limit     int        `json:"limit,omitempty"`
	Messages  int64      `json:"messages,omitempty"`
	Succeeded int64      `json:"succeeded,omitempty"`
	Skipped   int64      `json:"skipped,omitempty"`
	Failed    int64      `json:"failed,omitempty"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	JobID     string     `json:"job_id,omitempty"`
}

// DefaultMaxEntries bounds the in-memory ring when no explicit size is set.
const DefaultMaxEntries = 1000

// Store keeps audit entries in a bounded ring and, when configured,
// appends them to a JSONL file. File records are immutable snapshots:
// an updated entry (a finished replay) is appended again, and on load
// the last record per ID wins.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	max     int
	file    *os.File
}

// NewStore creates a store with an in-memory ring of max entries
// (DefaultMaxEntries when max <= 0). When filePath is non-empty, the
// store loads existing entries from it and appends new ones. The file
// is created with 0600 because entries may contain queue names and
// user names that should not be world-readable.
func NewStore(max int, filePath string) (*Store, error) {
	if max <= 0 {
		max = DefaultMaxEntries
	}
	s := &Store{max: max}
	if filePath == "" {
		return s, nil
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	s.file = f
	if err := s.loadFile(filePath); err != nil {
		_ = s.file.Close()
		s.file = nil
		return nil, err
	}
	return s, nil
}

// Close releases the audit file, if any.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// Record stores e, assigning an ID and a timestamp when missing, and
// returns the stored entry.
func (s *Store) Record(e Entry) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = newID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	s.append(e)
	s.persist(e)
	return e
}

// Update mutates the entry with the given id in place and appends the
// updated snapshot to the audit file. It reports whether the entry exists.
func (s *Store) Update(id string, fn func(*Entry)) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].ID != id {
			continue
		}
		fn(&s.entries[i])
		stored := s.entries[i]
		s.persist(stored)
		return stored, true
	}
	return Entry{}, false
}

// List returns a copy of all entries, newest first.
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.entries))
	for i, e := range s.entries {
		out[len(s.entries)-1-i] = e
	}
	return out
}

// append adds e to the ring; caller must hold s.mu.
func (s *Store) append(e Entry) {
	s.entries = append(s.entries, e)
	if len(s.entries) > s.max {
		s.entries = s.entries[len(s.entries)-s.max:]
	}
}

// persist appends e as one JSON line; caller must hold s.mu.
func (s *Store) persist(e Entry) {
	if s.file == nil {
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = s.file.Write(append(line, '\n'))
}

// loadFile reads existing entries from path. The last record per ID
// wins (replays append a snapshot when they finish). Malformed lines
// are skipped: they can only come from a torn write during a crash.
func (s *Store) loadFile(path string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // path comes from the operator-provided config
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read audit file: %w", err)
	}
	byID := map[string]Entry{}
	order := []string{}
	for _, line := range splitLines(raw) {
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil || e.ID == "" {
			continue
		}
		if _, seen := byID[e.ID]; !seen {
			order = append(order, e.ID)
		}
		byID[e.ID] = e
	}
	loaded := make([]Entry, 0, len(order))
	for _, id := range order {
		loaded = append(loaded, byID[id])
	}
	// Newest last, then keep only the max newest entries.
	sort.Slice(loaded, func(i, j int) bool {
		return loaded[i].Time.Before(loaded[j].Time)
	})
	if len(loaded) > s.max {
		loaded = loaded[len(loaded)-s.max:]
	}
	s.entries = loaded
	return nil
}

func splitLines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				lines = append(lines, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	return lines
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
