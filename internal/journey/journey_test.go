package journey

import (
	"reflect"
	"testing"
	"time"
)

func headers(xDeath []any, extra map[string]any) map[string]any {
	h := map[string]any{"x-death": xDeath}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func TestBuildNoDeath(t *testing.T) {
	if Build(nil, "q") != nil {
		t.Fatal("nil headers must give nil journey")
	}
	if Build(map[string]any{}, "q") != nil {
		t.Fatal("empty headers must give nil journey")
	}
	if Build(map[string]any{"x-death": []any{}}, "q") != nil {
		t.Fatal("empty x-death must give nil journey")
	}
}

func TestBuildSimpleChain(t *testing.T) {
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z", "exchange": "work", "routing-keys": []any{"b"}},
		map[string]any{"queue": "retry.1", "reason": "expired", "count": 1, "time": "2026-10-25T09:00:00Z", "exchange": "retry", "routing-keys": []any{"a"}},
		map[string]any{"queue": "work", "reason": "rejected", "count": 1, "time": "2026-10-25T08:00:00Z", "exchange": "main", "routing-keys": []any{"job"}},
	}, nil)
	j := Build(h, "dlq")
	if j == nil {
		t.Fatal("want journey")
	}
	if j.OriginQueue != "work" || j.OriginExchange != "main" {
		t.Fatalf("origin: %+v", j)
	}
	if !reflect.DeepEqual(j.OriginKeys, []string{"job"}) {
		t.Fatalf("origin keys: %v", j.OriginKeys)
	}
	if len(j.Hops) != 3 {
		t.Fatalf("want 3 hops, got %d", len(j.Hops))
	}
	// oldest first
	if j.Hops[0].Queue != "work" || j.Hops[2].Queue != "dlq" {
		t.Fatalf("hop order: %s -> %s", j.Hops[0].Queue, j.Hops[2].Queue)
	}
	if j.TotalDeaths != 3 || j.Retries != 0 {
		t.Fatalf("totals: deaths=%d retries=%d", j.TotalDeaths, j.Retries)
	}
	if len(j.Ladders) != 0 {
		t.Fatalf("no ladder expected: %+v", j.Ladders)
	}
	if j.CurrentQueue != "dlq" {
		t.Fatalf("current queue: %q", j.CurrentQueue)
	}
	if j.LastDeathTime != "2026-10-25T10:00:00Z" {
		t.Fatalf("last death time: %q", j.LastDeathTime)
	}
}

func TestBuildLadder(t *testing.T) {
	// Classic TTL+DLX ladder: work died expired three times, then landed
	// in dlq. x-death collapses the repeated expired deaths into one
	// entry with count=3.
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z", "exchange": "work"},
		map[string]any{"queue": "work", "reason": "expired", "count": 3, "time": "2026-10-25T09:00:00Z", "exchange": "work"},
	}, nil)
	j := Build(h, "dlq")
	if j == nil {
		t.Fatal("want journey")
	}
	if j.TotalDeaths != 4 || j.Retries != 2 {
		t.Fatalf("totals: deaths=%d retries=%d", j.TotalDeaths, j.Retries)
	}
	if len(j.Ladders) != 1 {
		t.Fatalf("want 1 ladder, got %+v", j.Ladders)
	}
	l := j.Ladders[0]
	if l.Queue != "work" || l.Attempts != 3 || !l.Cycle {
		t.Fatalf("ladder: %+v", l)
	}
	if !reflect.DeepEqual(l.Reasons, []string{"expired"}) {
		t.Fatalf("ladder reasons: %v", l.Reasons)
	}
}

func TestBuildLadderDedup(t *testing.T) {
	// Same queue listed twice in the chain must produce one ladder, not two.
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z"},
		map[string]any{"queue": "retry", "reason": "expired", "count": 2, "time": "2026-10-25T09:00:00Z"},
		map[string]any{"queue": "retry", "reason": "expired", "count": 1, "time": "2026-10-25T08:00:00Z"},
	}, nil)
	j := Build(h, "dlq")
	if len(j.Ladders) != 1 {
		t.Fatalf("want 1 ladder, got %+v", j.Ladders)
	}
	if j.Ladders[0].Attempts != 3 {
		t.Fatalf("attempts: %d", j.Ladders[0].Attempts)
	}
}

func TestBuildMixedReasonsNotCycle(t *testing.T) {
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z"},
		map[string]any{"queue": "work", "reason": "expired", "count": 2, "time": "2026-10-25T09:00:00Z"},
		map[string]any{"queue": "work", "reason": "rejected", "count": 1, "time": "2026-10-25T08:00:00Z"},
	}, nil)
	j := Build(h, "dlq")
	if len(j.Ladders) != 1 || j.Ladders[0].Cycle {
		t.Fatalf("want non-cycle ladder, got %+v", j.Ladders)
	}
	if j.Ladders[0].Attempts != 3 {
		t.Fatalf("attempts: %d", j.Ladders[0].Attempts)
	}
	if !reflect.DeepEqual(j.Ladders[0].Reasons, []string{"rejected", "expired"}) {
		t.Fatalf("reasons: %v", j.Ladders[0].Reasons)
	}
}

func TestBuildAgeSec(t *testing.T) {
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z"},
	}, nil)
	j := Build(h, "dlq")
	if j.AgeSec <= 0 {
		t.Fatalf("age must be positive, got %v", j.AgeSec)
	}
	if _, err := time.Parse(time.RFC3339, j.LastDeathTime); err != nil {
		t.Fatalf("last death time not RFC3339: %q", j.LastDeathTime)
	}
}

func TestBuildFirstDeathHeader(t *testing.T) {
	h := headers([]any{
		map[string]any{"queue": "dlq", "reason": "rejected", "count": 1, "time": "2026-10-25T10:00:00Z", "exchange": "work"},
	}, map[string]any{
		"x-first-death": map[string]any{"queue": "work", "exchange": "main", "routing-keys": []any{"job"}},
		"x-last-death":  map[string]any{"queue": "dlq", "exchange": "work"},
	})
	j := Build(h, "dlq")
	if j.OriginQueue != "work" || j.OriginExchange != "main" {
		t.Fatalf("origin from x-first-death: %+v", j)
	}
	if !reflect.DeepEqual(j.OriginKeys, []string{"job"}) {
		t.Fatalf("origin keys: %v", j.OriginKeys)
	}
}

func TestBuildMalformedEntries(t *testing.T) {
	h := headers([]any{"garbage", 42, map[string]any{"queue": "dlq", "reason": "rejected", "count": 1}}, nil)
	j := Build(h, "dlq")
	if j == nil {
		t.Fatal("want journey despite malformed entries")
	}
	if len(j.Hops) != 1 || j.Hops[0].Queue != "dlq" {
		t.Fatalf("hops: %+v", j.Hops)
	}
}
