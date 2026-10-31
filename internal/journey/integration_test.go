package journey

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
)

// TestRealDeadLetterJourney runs against a real broker when
// RMQ_CLUE_TEST_AMQP_URL is set: a message expires on a TTL queue with a
// dead-letter exchange, lands in the DLQ and its x-death chain must
// reconstruct into a one-hop journey marked as the origin.
func TestRealDeadLetterJourney(t *testing.T) {
	amqpURL := os.Getenv("RMQ_CLUE_TEST_AMQP_URL")
	if amqpURL == "" {
		t.Skip("RMQ_CLUE_TEST_AMQP_URL not set")
	}
	mgmtURL := envOrDefault("RMQ_CLUE_TEST_MGMT_URL", "http://127.0.0.1:15672")
	mgmtUser := envOrDefault("RMQ_CLUE_TEST_MGMT_USER", "guest")
	mgmtPass := envOrDefault("RMQ_CLUE_TEST_MGMT_PASS", "guest")

	session, err := amqpop.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer session.Close()

	if err := session.Channel.ExchangeDeclare("clue-journey", "direct", true, false, false, false, nil); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	dlq := "clue_journey_dlq"
	work := "clue_journey_work"
	clean := func() {
		_, _ = session.Channel.QueueDelete(dlq, false, false, false)
		_, _ = session.Channel.QueueDelete(work, false, false, false)
	}
	clean()
	if _, err := session.Channel.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		t.Fatalf("declare dlq: %v", err)
	}
	if _, err := session.Channel.QueueDeclare(work, true, false, false, false, amqp091.Table{
		"x-dead-letter-exchange": "clue-journey",
		"x-message-ttl":          int32(200),
	}); err != nil {
		t.Fatalf("declare work: %v", err)
	}
	t.Cleanup(clean)
	if err := session.Channel.QueueBind(dlq, work, "clue-journey", false, nil); err != nil {
		t.Fatalf("bind dlq: %v", err)
	}
	if _, err := session.Channel.QueuePurge(dlq, false); err != nil {
		t.Fatalf("purge: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := session.Publish(ctx, amqpop.Publishing{
		Exchange:   "",
		RoutingKey: work,
		Body:       []byte(`{"event":"journey"}`),
	}, true); err != nil {
		t.Fatalf("publish: %v", err)
	}

	client := mgmt.New(mgmtURL, mgmtUser, mgmtPass)
	var msgs []mgmt.Message
	deadline := time.Now().Add(10 * time.Second)
	for {
		msgs, err = client.Peek(context.Background(), "/", dlq, 10, 512*1024)
		if err == nil && len(msgs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dlq did not receive the expired message: %v (got %d)", err, len(msgs))
		}
		time.Sleep(300 * time.Millisecond)
	}

	headers, ok := msgs[0].Properties["headers"].(map[string]any)
	if !ok {
		t.Fatalf("no headers in peeked message: %+v", msgs[0].Properties)
	}
	j := Build(headers, dlq)
	if j == nil {
		t.Fatal("want journey from real x-death headers")
	}
	if j.OriginQueue != work {
		t.Fatalf("origin queue: want %s, got %q", work, j.OriginQueue)
	}
	if len(j.Hops) != 1 || j.Hops[0].Queue != work || j.Hops[0].Reason != "expired" {
		t.Fatalf("hops: %+v", j.Hops)
	}
	if j.Hops[0].Time == "" {
		t.Fatalf("hop time missing: %+v", j.Hops[0])
	}
	if !reflect.DeepEqual(j.Hops[0].RoutingKeys, []string{work}) {
		t.Fatalf("hop routing keys: %+v", j.Hops[0])
	}
	if j.CurrentQueue != dlq || j.AgeSec <= 0 {
		t.Fatalf("current=%q age=%v", j.CurrentQueue, j.AgeSec)
	}
	if len(j.Ladders) != 0 {
		t.Fatalf("no ladder expected: %+v", j.Ladders)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
