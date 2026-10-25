package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/saromanov/rabbitmq-clue/internal/config"
)

func fakeMgmt(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/overview":
			_, _ = w.Write([]byte(`{"management_version":"4.2.5","rabbitmq_version":"4.2.5","cluster_name":"test","queues":3,
				"queue_totals":{"messages":42,"messages_ready":40,"messages_unacknowledged":2},
				"object_totals":{"connections":7,"channels":9,"consumers":5,"queues":3,"exchanges":10}}`))
		case "/api/queues/%2F":
			_, _ = w.Write([]byte(`[{"name":"work","vhost":"/","messages":10,"consumers":2}]`))
		case "/api/vhosts":
			_, _ = w.Write([]byte(`[{"name":"/","tracing":false},{"name":"staging","tracing":false}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		}
	}))
}

func testConfig(t *testing.T, mgmtURL string, readOnly bool) *config.Config {
	t.Helper()
	return &config.Config{
		Addr: "127.0.0.1:0",
		Connections: []config.Connection{
			{Name: "test", MgmtURL: mgmtURL, AMQPURL: "amqp://127.0.0.1:1/%2F", Username: "u", Password: "p"},
		},
		ReadOnly: readOnly,
	}
}

func TestHealthAndConnections(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connections", nil))
	var conns []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &conns); err != nil {
		t.Fatalf("connections: %v", err)
	}
	if len(conns) != 1 || conns[0]["name"] != "test" {
		t.Fatalf("connections: %v", conns)
	}
}

func TestQueuesProxied(t *testing.T) {
	fake := fakeMgmt(t)
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/queues?vhost=%2F", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("queues: %d %s", rec.Code, rec.Body.String())
	}
	var queues []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &queues); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(queues) != 1 || queues[0]["name"] != "work" {
		t.Fatalf("queues: %v", queues)
	}
}

func TestUnknownConnection(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/queues?conn=ghost", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestReadOnlyGuard(t *testing.T) {
	srv := New(testConfig(t, "http://unused", true))
	for _, path := range []string{"/api/queues/purge", "/api/replay", "/api/publish"} {
		rec := httptest.NewRecorder()
		srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", path, rec.Code)
		}
	}
	// GET endpoints remain accessible in read-only mode.
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/queues", nil))
	if rec.Code == http.StatusForbidden {
		t.Fatal("GET must not be blocked by read-only guard")
	}
}

func TestReplayJobLifecycle(t *testing.T) {
	fake := fakeMgmt(t)
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	body := `{"conn":"test","vhost":"/","source_queue":"dlq","target_queue":"work","dry_run":true}`
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/replay", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("replay start: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	jobID := resp["job_id"]
	if jobID == "" {
		t.Fatal("want job id")
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/replay/jobs/"+url.PathEscape(jobID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("job status: %d", rec.Code)
	}
	var job map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job["status"] != "running" && job["status"] != "failed" {
		t.Fatalf("unexpected status: %v", job["status"])
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/replay/jobs/zzz", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown job: %d", rec.Code)
	}
}

func TestPeekValidation(t *testing.T) {
	fake := fakeMgmt(t)
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/peek", strings.NewReader("{bad json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}
}
