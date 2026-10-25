package replay

import (
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestMatches(t *testing.T) {
	d := amqp091.Delivery{Body: []byte(`{"error":"timeout"}`), Headers: map[string]any{"reason": "timeout"}}

	cases := []struct {
		name    string
		filters []Filter
		want    bool
	}{
		{"no filters", nil, true},
		{"substring hit", []Filter{{Kind: "substring", Value: "timeout"}}, true},
		{"substring miss", []Filter{{Kind: "substring", Value: "panic"}}, false},
		{"header hit", []Filter{{Kind: "header", Header: "reason", Value: "timeout"}}, true},
		{"header miss value", []Filter{{Kind: "header", Header: "reason", Value: "panic"}}, false},
		{"header miss key", []Filter{{Kind: "header", Header: "nope", Value: "timeout"}}, false},
		{"both hit", []Filter{{Kind: "substring", Value: "error"}, {Kind: "header", Header: "reason", Value: "timeout"}}, true},
		{"one miss", []Filter{{Kind: "substring", Value: "error"}, {Kind: "header", Header: "reason", Value: "panic"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matches(d, tc.filters); got != tc.want {
				t.Fatalf("matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToPublishing(t *testing.T) {
	src := amqp091.Delivery{
		Body:         []byte("payload"),
		RoutingKey:   "original.key",
		ContentType:  "application/json",
		Headers:      map[string]any{"x-death": []any{"x"}, "keep": "yes"},
		DeliveryMode: 2,
		Priority:     5,
	}

	t.Run("target queue overrides routing key", func(t *testing.T) {
		p := toPublishing(src, "work", "", "", false)
		if p.RoutingKey != "work" {
			t.Fatalf("routing key: %q", p.RoutingKey)
		}
		if p.Exchange != "" {
			t.Fatalf("exchange: %q", p.Exchange)
		}
	})

	t.Run("target exchange keeps routing key", func(t *testing.T) {
		p := toPublishing(src, "", "main", "", false)
		if p.Exchange != "main" || p.RoutingKey != "original.key" {
			t.Fatalf("exchange/key: %q/%q", p.Exchange, p.RoutingKey)
		}
	})

	t.Run("explicit routing key wins", func(t *testing.T) {
		p := toPublishing(src, "", "main", "forced.key", false)
		if p.RoutingKey != "forced.key" {
			t.Fatalf("routing key: %q", p.RoutingKey)
		}
	})

	t.Run("markers added and x-death strip", func(t *testing.T) {
		p := toPublishing(src, "work", "", "", true)
		if _, ok := p.Headers["x-death"]; ok {
			t.Fatal("x-death must be stripped")
		}
		if p.Headers["keep"] != "yes" {
			t.Fatal("custom headers must be preserved")
		}
		if p.Headers["x-replayed-by"] != "rabbitmq-clue" {
			t.Fatal("replay marker missing")
		}
		if _, ok := p.Headers["x-replay-time"]; !ok {
			t.Fatal("replay time missing")
		}
	})

	t.Run("x-death kept by default", func(t *testing.T) {
		p := toPublishing(src, "work", "", "", false)
		if _, ok := p.Headers["x-death"]; !ok {
			t.Fatal("x-death must be preserved when strip disabled")
		}
	})
}

func TestEngineValidation(t *testing.T) {
	e := NewEngine()
	if _, err := e.Start("amqp://localhost:1", Spec{SourceQueue: "dlq"}); err == nil {
		t.Fatal("want error without target")
	}
	if _, err := e.Start("amqp://localhost:1", Spec{TargetQueue: "work"}); err == nil {
		t.Fatal("want error without source")
	}
	if _, err := e.Start("amqp://localhost:1", Spec{SourceQueue: "dlq", TargetQueue: "work"}); err != nil {
		t.Fatalf("valid spec: %v", err)
	}
	if len(e.List()) != 1 {
		t.Fatalf("jobs: %d", len(e.List()))
	}
}

func TestJobLifecyclesToFailed(t *testing.T) {
	e := NewEngine()
	// Port 1 is unreachable: the dial must fail and the job must turn failed.
	id, err := e.Start("amqp://127.0.0.1:1/%2F", Spec{SourceQueue: "dlq", TargetQueue: "work"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	job, ok := waitStatus(t, e, id, "failed")
	if !ok {
		return
	}
	if len(job.Errors) == 0 {
		t.Fatal("want recorded error")
	}
	if job.EndedAt == nil {
		t.Fatal("want ended_at")
	}
}

func waitStatus(t *testing.T, e *Engine, id, want string) (Job, bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		job, ok := e.Get(id)
		if !ok {
			t.Fatalf("job %q vanished", id)
		}
		if job.Status == want {
			return job, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %q never reached status %q", id, want)
	return Job{}, false
}
