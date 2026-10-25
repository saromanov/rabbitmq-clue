package replay

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
)

// TestReplayIntegration runs against a real broker when
// RMQ_CLUE_TEST_AMQP_URL is set (CI provides a rabbitmq service container).
func TestReplayIntegration(t *testing.T) {
	amqpURL := os.Getenv("RMQ_CLUE_TEST_AMQP_URL")
	if amqpURL == "" {
		t.Skip("RMQ_CLUE_TEST_AMQP_URL not set")
	}

	session, err := amqpop.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer session.Close()

	for _, q := range []string{"clue_dlq", "clue_work"} {
		if _, err := session.Channel.QueueDeclare(q, true, false, false, false, nil); err != nil {
			t.Fatalf("declare %s: %v", q, err)
		}
		if _, err := session.Channel.QueuePurge(q, false); err != nil {
			t.Fatalf("purge %s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = session.Channel.QueueDelete("clue_dlq", false, false, false)
		_, _ = session.Channel.QueueDelete("clue_work", false, false, false)
	})

	for i := 0; i < 5; i++ {
		err := session.Publish(context.Background(), amqpop.Publishing{
			Exchange:   "",
			RoutingKey: "clue_dlq",
			Body:       []byte(`{"error":"boom","seq":1}`),
			Headers:    map[string]any{"reason": "timeout"},
		}, true)
		if err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	e := NewEngine()
	id, err := e.Start(amqpURL, Spec{
		SourceQueue: "clue_dlq",
		TargetQueue: "clue_work",
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitIntegration(t, e, id, "completed")
	if job.Progress.Succeeded != 5 {
		t.Fatalf("succeeded: want 5, got %d", job.Progress.Succeeded)
	}

	// Messages must have arrived in the target with the replay marker.
	deliveries, err := session.GetMany("clue_work", 10)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if len(deliveries) != 5 {
		t.Fatalf("work queue: want 5 messages, got %d", len(deliveries))
	}
	first := deliveries[0]
	if first.Headers["x-replayed-by"] != "rabbitmq-clue" {
		t.Fatalf("replay marker missing: %v", first.Headers)
	}
	if string(first.Body) != `{"error":"boom","seq":1}` {
		t.Fatalf("body mismatch: %q", first.Body)
	}

	// Source must be empty afterwards.
	_, ok, err := session.GetOne("clue_dlq")
	if err != nil {
		t.Fatalf("get dlq: %v", err)
	}
	if ok {
		t.Fatal("dlq must be drained")
	}
}

// TestReplayIntegrationFiltered verifies that non-matching messages stay
// in the source queue while matching ones are replayed.
func TestReplayIntegrationFiltered(t *testing.T) {
	amqpURL := os.Getenv("RMQ_CLUE_TEST_AMQP_URL")
	if amqpURL == "" {
		t.Skip("RMQ_CLUE_TEST_AMQP_URL not set")
	}

	session, err := amqpop.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer session.Close()

	for _, q := range []string{"clue_f_dlq", "clue_f_work"} {
		if _, err := session.Channel.QueueDeclare(q, true, false, false, false, nil); err != nil {
			t.Fatalf("declare %s: %v", q, err)
		}
		if _, err := session.Channel.QueuePurge(q, false); err != nil {
			t.Fatalf("purge %s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = session.Channel.QueueDelete("clue_f_dlq", false, false, false)
		_, _ = session.Channel.QueueDelete("clue_f_work", false, false, false)
	})

	publish := func(seq int, reason string) {
		t.Helper()
		err := session.Publish(context.Background(), amqpop.Publishing{
			Exchange:   "",
			RoutingKey: "clue_f_dlq",
			Body:       []byte(`{"reason":"` + reason + `"}`),
			Headers:    map[string]any{"reason": reason},
		}, true)
		if err != nil {
			t.Fatalf("publish %d: %v", seq, err)
		}
	}
	publish(1, "timeout")
	publish(2, "panic")
	publish(3, "timeout")

	e := NewEngine()
	id, err := e.Start(amqpURL, Spec{
		SourceQueue: "clue_f_dlq",
		TargetQueue: "clue_f_work",
		Filters:     []Filter{{Kind: "header", Header: "reason", Value: "timeout"}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitIntegration(t, e, id, "completed")
	if job.Progress.Succeeded != 2 {
		t.Fatalf("succeeded: want 2, got %d", job.Progress.Succeeded)
	}
	if job.Progress.Skipped != 1 {
		t.Fatalf("skipped: want 1, got %d", job.Progress.Skipped)
	}

	remaining, err := session.GetMany("clue_f_dlq", 10)
	if err != nil {
		t.Fatalf("get dlq: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Headers["reason"] != "panic" {
		t.Fatalf("remaining: %+v", remaining)
	}
}

// TestReplayIntegrationDryRun verifies that a dry run counts matches and
// leaves every message in the source queue.
func TestReplayIntegrationDryRun(t *testing.T) {
	amqpURL := os.Getenv("RMQ_CLUE_TEST_AMQP_URL")
	if amqpURL == "" {
		t.Skip("RMQ_CLUE_TEST_AMQP_URL not set")
	}

	session, err := amqpop.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer session.Close()

	const q = "clue_dry_dlq"
	if _, err := session.Channel.QueueDeclare(q, true, false, false, false, nil); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if _, err := session.Channel.QueuePurge(q, false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	t.Cleanup(func() {
		_, _ = session.Channel.QueueDelete(q, false, false, false)
	})

	for _, reason := range []string{"timeout", "panic", "timeout"} {
		err := session.Publish(context.Background(), amqpop.Publishing{
			Exchange:   "",
			RoutingKey: q,
			Body:       []byte(`{"reason":"` + reason + `"}`),
			Headers:    map[string]any{"reason": reason},
		}, true)
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	e := NewEngine()
	id, err := e.Start(amqpURL, Spec{
		SourceQueue: q,
		TargetQueue: "clue_dry_work",
		DryRun:      true,
		Filters:     []Filter{{Kind: "header", Header: "reason", Value: "timeout"}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitIntegration(t, e, id, "completed")
	if job.Progress.Succeeded != 2 {
		t.Fatalf("dry-run succeeded: want 2, got %d", job.Progress.Succeeded)
	}
	remaining, err := session.GetMany(q, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("dry run must keep all messages, got %d", len(remaining))
	}
}

func waitIntegration(t *testing.T, e *Engine, id, want string) Job {
	t.Helper()
	for i := 0; i < 500; i++ {
		job, ok := e.Get(id)
		if !ok {
			t.Fatalf("job vanished")
		}
		if job.Status == "failed" || job.Status == "cancelled" {
			t.Fatalf("job failed: %v", job.Errors)
		}
		if job.Status == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job never reached %q", want)
	return Job{}
}
