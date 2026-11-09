package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saromanov/rabbitmq-clue/internal/audit"
)

// fakeMgmtForPurge serves a single queue with a depth and accepts purges.
func fakeMgmtForPurge(t *testing.T, purged *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/queues/%2F/dlq":
			_, _ = w.Write([]byte(`{"name":"dlq","vhost":"/","messages":7,"consumers":0}`))
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/api/queues/%2F/dlq/contents":
			*purged++
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		}
	}))
}

func TestPurgeRecordedWithDepth(t *testing.T) {
	purged := 0
	fake := fakeMgmtForPurge(t, &purged)
	defer fake.Close()
	srv := mustNew(t, testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	body := `{"conn":"test","vhost":"/","name":"dlq"}`
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/purge", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("purge: %d %s", rec.Code, rec.Body.String())
	}

	entries := srv.audit.List()
	if len(entries) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Operation != audit.OpPurge || e.Queue != "dlq" || e.VHost != "/" || e.Conn != "test" {
		t.Fatalf("entry fields: %+v", e)
	}
	if e.Messages != 7 || e.Status != audit.StatusCompleted {
		t.Fatalf("want depth 7 completed, got %+v", e)
	}
	if e.User == "" {
		t.Fatal("want actor recorded")
	}
}

func TestPurgeFailureRecorded(t *testing.T) {
	purged := 0
	fake := fakeMgmtForPurge(t, &purged)
	defer fake.Close()
	srv := mustNew(t, testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	// Unknown queue: the depth probe fails, purge hits the fake's 404.
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/purge", strings.NewReader(`{"conn":"test","vhost":"/","name":"ghost"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
	entries := srv.audit.List()
	if len(entries) != 1 || entries[0].Status != audit.StatusFailed || entries[0].Error == "" {
		t.Fatalf("want failed entry, got %+v", entries)
	}
}

func TestAuditActorFromHeaderAndFallback(t *testing.T) {
	purged := 0
	fake := fakeMgmtForPurge(t, &purged)
	defer fake.Close()

	cfg := testConfig(t, fake.URL, false)
	cfg.AuditUserHeader = "X-Forwarded-User"
	srv := mustNew(t, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/queues/purge", strings.NewReader(`{"conn":"test","vhost":"/","name":"dlq"}`))
	req.Header.Set("X-Forwarded-User", "alice")
	srv.Handler(nil).ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodPost, "/api/queues/purge", strings.NewReader(`{"conn":"test","vhost":"/","name":"dlq"}`))
	srv.Handler(nil).ServeHTTP(httptest.NewRecorder(), req)

	entries := srv.audit.List()
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].User != "192.0.2.1" {
		t.Fatalf("newest entry should be IP fallback, got %q", entries[0].User)
	}
	if entries[1].User != "alice" {
		t.Fatalf("want user from header, got %q", entries[1].User)
	}
	if entries[1].Remote == "" {
		t.Fatal("want remote recorded")
	}
}

func TestReplayAuditLifecycle(t *testing.T) {
	srv := mustNew(t, testConfig(t, "http://unused", false))

	body := `{"conn":"test","vhost":"/","source_queue":"dlq","target_queue":"work","limit":5,"filters":[{"kind":"substring","value":"order"}]}`
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/replay", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body.String())
	}

	// Dial fails fast (unreachable AMQP): wait for the finish callback
	// to fold the final state into the audit entry.
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries := srv.audit.List()
		if len(entries) == 1 && entries[0].Status == audit.StatusFailed {
			e := entries[0]
			if e.Operation != audit.OpReplay || e.Queue != "dlq" || e.TargetQueue != "work" {
				t.Fatalf("entry fields: %+v", e)
			}
			if e.Limit != 5 || len(e.Filters) != 1 || e.Filters[0].Kind != "substring" {
				t.Fatalf("spec fields: %+v", e)
			}
			if e.JobID == "" || e.Error == "" {
				t.Fatalf("want job id and error: %+v", e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("audit entry never finalized: %+v", srv.audit.List())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAuditEndpointFilters(t *testing.T) {
	srv := mustNew(t, testConfig(t, "http://unused", false))
	srv.audit.Record(audit.Entry{ID: "1", Operation: audit.OpPurge, Queue: "q1", Status: audit.StatusCompleted})
	srv.audit.Record(audit.Entry{ID: "2", Operation: audit.OpReplay, Queue: "q2", Status: audit.StatusRunning})
	srv.audit.Record(audit.Entry{ID: "3", Operation: audit.OpPublish, Queue: "rk", Status: audit.StatusCompleted})

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit", nil))
	var all []audit.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 3 || all[0].ID != "3" {
		t.Fatalf("want newest first, got %+v", all)
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?operation=purge", nil))
	var purges []audit.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &purges); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(purges) != 1 || purges[0].ID != "1" {
		t.Fatalf("operation filter: %+v", purges)
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?limit=2", nil))
	var limited []audit.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &limited); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(limited) != 2 || limited[0].ID != "3" || limited[1].ID != "2" {
		t.Fatalf("limit: %+v", limited)
	}
}

func TestPublishRecorded(t *testing.T) {
	srv := mustNew(t, testConfig(t, "http://unused", false))

	rec := httptest.NewRecorder()
	body := `{"conn":"test","vhost":"/","exchange":"orders","routing_key":"order.created","body":"{}"}`
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/publish", strings.NewReader(body)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("dial must fail against unreachable AMQP, got %d", rec.Code)
	}
	entries := srv.audit.List()
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Operation != audit.OpPublish || e.Status != audit.StatusFailed || e.Error == "" {
		t.Fatalf("entry: %+v", e)
	}
	if e.Queue != "order.created" || e.TargetExchange != "orders" || e.Messages != 1 {
		t.Fatalf("publish fields: %+v", e)
	}
}
