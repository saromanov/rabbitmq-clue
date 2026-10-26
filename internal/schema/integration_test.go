package schema

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
)

// TestPublishPeekRoundTrip runs against a real broker when
// RMQ_CLUE_TEST_AMQP_URL is set: publish protobuf payloads by schema,
// peek them through the management API and decode them back to JSON.
func TestPublishPeekRoundTrip(t *testing.T) {
	amqpURL := os.Getenv("RMQ_CLUE_TEST_AMQP_URL")
	if amqpURL == "" {
		t.Skip("RMQ_CLUE_TEST_AMQP_URL not set")
	}
	mgmtURL := envOrDefault("RMQ_CLUE_TEST_MGMT_URL", "http://127.0.0.1:15672")
	mgmtUser := envOrDefault("RMQ_CLUE_TEST_MGMT_USER", "guest")
	mgmtPass := envOrDefault("RMQ_CLUE_TEST_MGMT_PASS", "guest")

	src, err := os.ReadFile("testdata/test.proto")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	c, err := Compile(map[string]string{"test.proto": string(src)})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	session, err := amqpop.Dial(amqpURL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer session.Close()
	queue := "clue_schema"
	if _, err := session.Channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if _, err := session.Channel.QueuePurge(queue, false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	t.Cleanup(func() {
		_, _ = session.Channel.QueueDelete(queue, false, false, false)
	})

	itemJSON := []byte(`{"stringField":"round-trip","int32Field":9,"tags":["x","y"],"address":{"city":"Kazan"}}`)
	itemRes, err := c.Encode("clue.test.v1.Item", itemJSON, EncodeOpts{})
	if err != nil {
		t.Fatalf("encode item: %v", err)
	}
	anyRes, err := c.Encode("clue.test.v1.Address", []byte(`{"city":"SPb"}`), EncodeOpts{WrapAny: true})
	if err != nil {
		t.Fatalf("encode any: %v", err)
	}

	for _, p := range []*EncodeResult{itemRes, anyRes} {
		if err := session.Publish(context.Background(), amqpop.Publishing{
			Exchange:    "",
			RoutingKey:  queue,
			Body:        p.Body,
			ContentType: p.ContentType,
		}, true); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	client := mgmt.New(mgmtURL, mgmtUser, mgmtPass)
	var msgs []mgmt.Message
	deadline := time.Now().Add(10 * time.Second)
	for {
		msgs, err = client.Peek(context.Background(), "/", queue, 10, 512*1024)
		if err == nil && len(msgs) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("peek did not return 2 messages: %v (got %d)", err, len(msgs))
		}
		time.Sleep(300 * time.Millisecond)
	}

	decodedItem, err := c.Decode("clue.test.v1.Item", decodePayload(t, msgs[0]), DecodeOpts{})
	if err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if decodedItem.Unwrapped {
		t.Error("item decode should not unwrap")
	}
	for _, want := range []string{`"stringField": "round-trip"`, `"city": "Kazan"`, `"x"`, `"y"`} {
		if !strings.Contains(string(decodedItem.Decoded), want) {
			t.Errorf("decoded item missing %s: %s", want, decodedItem.Decoded)
		}
	}

	decodedAny, err := c.Decode("clue.test.v1.Address", decodePayload(t, msgs[1]), DecodeOpts{})
	if err != nil {
		t.Fatalf("decode any: %v", err)
	}
	if !decodedAny.Unwrapped || decodedAny.DecodedType != "clue.test.v1.Address" {
		t.Fatalf("any decode: %+v", decodedAny)
	}
	if !strings.Contains(string(decodedAny.Decoded), `"city": "SPb"`) {
		t.Errorf("decoded any = %s", decodedAny.Decoded)
	}
}

func decodePayload(t *testing.T, m mgmt.Message) []byte {
	t.Helper()
	if strings.EqualFold(m.PayloadEncoding, "base64") {
		raw, err := base64.StdEncoding.DecodeString(m.Payload)
		if err != nil {
			t.Fatalf("base64 payload: %v", err)
		}
		return raw
	}
	return []byte(m.Payload)
}

func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
