package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordAssignsIDAndTime(t *testing.T) {
	s, err := NewStore(0, "")
	if err != nil {
		t.Fatal(err)
	}
	stored := s.Record(Entry{Operation: OpPurge, Queue: "dlq", Status: StatusCompleted})
	if stored.ID == "" {
		t.Fatal("want id")
	}
	if stored.Time.IsZero() {
		t.Fatal("want time")
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != stored.ID {
		t.Fatalf("want one entry %v, got %v", stored, list)
	}
	if list[0].Operation != OpPurge {
		t.Fatalf("want newest first, got %v", list)
	}
}

func TestRecordKeepsExplicitIDAndTime(t *testing.T) {
	s, _ := NewStore(0, "")
	when := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	stored := s.Record(Entry{ID: "fixed", Time: when, Status: StatusCompleted})
	if stored.ID != "fixed" || !stored.Time.Equal(when) {
		t.Fatalf("explicit fields lost: %v", stored)
	}
}

func TestRingDropsOldest(t *testing.T) {
	s, _ := NewStore(3, "")
	for i := 0; i < 5; i++ {
		s.Record(Entry{ID: string(rune('a' + i)), Status: StatusCompleted})
	}
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("want 3 entries, got %d", len(list))
	}
	// Newest first: e, d, c.
	if list[0].ID != "e" || list[2].ID != "c" {
		t.Fatalf("want e,d,c, got %v", list)
	}
}

func TestUpdateMutatesExisting(t *testing.T) {
	s, _ := NewStore(0, "")
	stored := s.Record(Entry{Operation: OpReplay, Status: StatusRunning})
	done := time.Now().UTC()
	updated, ok := s.Update(stored.ID, func(e *Entry) {
		e.Status = StatusCompleted
		e.EndedAt = &done
		e.Succeeded = 7
	})
	if !ok {
		t.Fatal("want updated entry")
	}
	if updated.Status != StatusCompleted || updated.Succeeded != 7 || updated.EndedAt == nil {
		t.Fatalf("update not applied: %v", updated)
	}
	if _, ok := s.Update("missing", func(*Entry) {}); ok {
		t.Fatal("update of missing entry must fail")
	}
}

func TestFilePersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	s1, err := NewStore(0, path)
	if err != nil {
		t.Fatal(err)
	}
	first := s1.Record(Entry{Operation: OpPurge, Queue: "q1", Status: StatusCompleted})
	running := s1.Record(Entry{Operation: OpReplay, Queue: "q1", TargetQueue: "q2", Status: StatusRunning})
	s1.Update(running.ID, func(e *Entry) {
		e.Status = StatusCompleted
		e.Succeeded = 3
	})
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(0, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	list := s2.List()
	if len(list) != 2 {
		t.Fatalf("want 2 deduped entries, got %d: %v", len(list), list)
	}
	// Newest first: finished replay, then purge.
	if list[0].Operation != OpReplay || list[0].Status != StatusCompleted || list[0].Succeeded != 3 {
		t.Fatalf("replay snapshot lost: %v", list[0])
	}
	if list[1].ID != first.ID || list[1].Operation != OpPurge {
		t.Fatalf("purge entry lost: %v", list[1])
	}
}

func TestFileLoadSkipsTornTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	good, _ := json.Marshal(Entry{ID: "ok1", Operation: OpPurge, Status: StatusCompleted})
	content := string(good) + "\n" + `{"id":"torn","stat`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(0, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list := s.List()
	if len(list) != 1 || list[0].ID != "ok1" {
		t.Fatalf("want only good entry, got %v", list)
	}
}

func TestFileLoadMissingFile(t *testing.T) {
	s, err := NewStore(10, filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.List()) != 0 {
		t.Fatal("want empty store")
	}
}

func TestFileLoadTrimsToMax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	var sb strings.Builder
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		line, _ := json.Marshal(Entry{ID: string(rune('a' + i)), Time: base.Add(time.Duration(i) * time.Minute), Status: StatusCompleted})
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(3, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("want 3 entries, got %d", len(list))
	}
	if list[0].ID != "e" || list[2].ID != "c" {
		t.Fatalf("want newest c,d,e kept, got %v", list)
	}
}

func TestConcurrentRecordAndUpdate(t *testing.T) {
	s, _ := NewStore(0, "")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := s.Record(Entry{Operation: OpPublish, Status: StatusCompleted})
			s.Update(e.ID, func(en *Entry) { en.Succeeded = 1 })
		}()
	}
	wg.Wait()
	if len(s.List()) != 20 {
		t.Fatalf("want 20 entries, got %d", len(s.List()))
	}
}

func TestListReturnsCopy(t *testing.T) {
	s, _ := NewStore(0, "")
	s.Record(Entry{Status: StatusCompleted})
	list := s.List()
	list[0].Status = "tampered"
	if s.List()[0].Status == "tampered" {
		t.Fatal("List must return a copy")
	}
}
