package mgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		switch r.URL.EscapedPath() {
		case "/api/queues/default":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"name":"work","vhost":"default","type":"classic","messages":5,"messages_ready":4,"messages_unacknowledged":1,"consumers":2,
				 "message_stats":{"publish":100,"deliver_get":80,"ack":70,"publish_details":{"rate":3.5}}},
				{"name":"dead","vhost":"default","type":"quorum","messages":1000}
			]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "user", "pass")
	queues, err := c.Queues(context.Background(), "default")
	if err != nil {
		t.Fatalf("Queues: %v", err)
	}
	if len(queues) != 2 {
		t.Fatalf("want 2 queues, got %d", len(queues))
	}
	q := queues[0]
	if q.Name != "work" || q.Type != "classic" || q.Messages != 5 || q.Consumers != 2 {
		t.Fatalf("unexpected queue: %+v", q)
	}
	if q.MessageStats == nil || q.MessageStats.Publish != 100 {
		t.Fatalf("unexpected message stats: %+v", q.MessageStats)
	}
}

func TestPeekAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.EscapedPath() == "/api/queues/%2F/dlq/get" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"payload":"e30=","payload_encoding":"base64","exchange":"ex","routing_key":"dlq","payload_bytes":2,
				"properties":{"content_type":"application/json","headers":{"x-death":[{"queue":"work","reason":"rejected","count":3,"time":"2026-10-25 10:00:00.000"}]}}}]`))
		case r.URL.EscapedPath() == "/api/queues/%2F/missing/contents" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found","reason":"no queue"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "user", "pass")
	msgs, err := c.Peek(context.Background(), "/", "dlq", 5, 1024)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(msgs) != 1 || msgs[0].PayloadEncoding != "base64" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if msgs[0].RoutingKey != "dlq" {
		t.Fatalf("unexpected routing key: %q", msgs[0].RoutingKey)
	}

	err = c.Purge(context.Background(), "/", "missing")
	if err == nil {
		t.Fatal("want error for 404 purge")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestVHostPathEscaping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/queues/my%2Fvhost" {
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p")
	if _, err := c.Queues(context.Background(), "my/vhost"); err != nil {
		t.Fatalf("Queues with slash vhost: %v", err)
	}
}

func TestForensics(t *testing.T) {
	msg := Message{Properties: map[string]any{
		"timestamp": map[string]any{"timestamp": 1770000000, "offset": 0},
		"headers": map[string]any{
			"x-death": []any{
				map[string]any{"queue": "retry.2", "reason": "expired", "count": 2, "time": "2026-10-25 10:00:00.000", "exchange": "work"},
				map[string]any{"queue": "work", "reason": "rejected", "count": 3, "time": "2026-10-25T09:00:00Z", "exchange": "main"},
			},
		},
	}}

	h := HeaderMap(msg.Properties["headers"].(map[string]any))
	chain := h.DeathChain()
	if len(chain) != 2 {
		t.Fatalf("want 2 death steps, got %d", len(chain))
	}
	if chain[0].Queue != "retry.2" || chain[0].Reason != "expired" || chain[0].Count != 2 {
		t.Fatalf("unexpected first step: %+v", chain[0])
	}
	if got := h.TotalDeaths(); got != 5 {
		t.Fatalf("total deaths: want 5, got %d", got)
	}
	if got := h.OriginQueue(); got != "work" {
		t.Fatalf("origin queue: want work, got %q", got)
	}
	last, ok := h.LastDeathTime()
	if !ok {
		t.Fatal("want last death time")
	}
	if want, _ := time.Parse("2006-01-02 15:04:05.000", "2026-10-25 10:00:00.000"); !last.Equal(want) {
		t.Fatalf("last death time: want %v, got %v", want, last)
	}
	head, ok := msg.HeadTime()
	if !ok || head.Unix() != 1770000000 {
		t.Fatalf("head time: %v ok=%v", head, ok)
	}
}

func TestHeadTimeFromXDeath(t *testing.T) {
	msg := Message{Properties: map[string]any{"headers": map[string]any{
		"x-death": []any{
			map[string]any{"queue": "work", "reason": "rejected", "count": 1, "time": "2026-10-25T09:00:00Z"},
		},
	}}}
	head, ok := msg.HeadTime()
	if !ok {
		t.Fatal("want head time from x-death")
	}
	if want, _ := time.Parse(time.RFC3339, "2026-10-25T09:00:00Z"); !head.Equal(want) {
		t.Fatalf("head time: want %v, got %v", want, head)
	}
}

func TestPolicies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/policies/default":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"name":"dlq","vhost":"default","apply-to":"queues","pattern":"^work$","priority":5,
				 "definition":{"dead-letter-exchange":"dlx","message-ttl":60000}}
			]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "user", "pass")
	policies, err := c.Policies(context.Background(), "default")
	if err != nil {
		t.Fatalf("Policies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("want 1 policy, got %d", len(policies))
	}
	p := policies[0]
	if p.Name != "dlq" || p.ApplyTo != "queues" || p.Pattern != "^work$" || p.Priority != 5 {
		t.Fatalf("unexpected policy: %+v", p)
	}
	if p.Definition["dead-letter-exchange"] != "dlx" {
		t.Fatalf("unexpected definition: %+v", p.Definition)
	}
}
