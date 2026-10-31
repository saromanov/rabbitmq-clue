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
		case "/api/exchanges/%2F":
			_, _ = w.Write([]byte(`[{"name":"orders","type":"topic","durable":true,"arguments":{}}]`))
		case "/api/bindings/%2F":
			_, _ = w.Write([]byte(`[{"source":"orders","destination":"work","destination_type":"queue","routing_key":"order.*","arguments":{}}]`))
		case "/api/policies/%2F":
			_, _ = w.Write([]byte(`[{"name":"dlq","vhost":"/","apply-to":"queues","pattern":"^work$","definition":{"dead-letter-exchange":"dlx"}}]`))
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
		Addr:       "127.0.0.1:0",
		SchemasDir: t.TempDir(),
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

func TestTopologySnapshot(t *testing.T) {
	fake := fakeMgmt(t)
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/topology/snapshot?vhost=%2F", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot: %d %s", rec.Code, rec.Body.String())
	}
	var snap map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap["format"] != "rabbitmq-clue.topology" || snap["version"] != float64(1) {
		t.Fatalf("bad snapshot header: %+v", snap)
	}
	if snap["vhost"] != "/" || snap["source"] != "test" {
		t.Fatalf("bad source/vhost: %+v", snap)
	}
	queues := snap["queues"].([]any)
	if len(queues) != 1 {
		t.Fatalf("want 1 queue: %+v", queues)
	}
}

func TestTopologySnapshotUnknownConn(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/topology/snapshot?conn=ghost&vhost=%2F", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// TestTopologyDiffInline compares two uploaded snapshots without touching
// a broker, in JSON and text formats.
func TestTopologyDiffInline(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))

	body := `{"from": {"snapshot": {"format": "rabbitmq-clue.topology", "version": 1, "source": "dev", "vhost": "/",
		"queues": [{"name": "work", "type": "classic", "durable": true, "arguments": {}}]}},
		"to": {"snapshot": {"format": "rabbitmq-clue.topology", "version": 1, "source": "prod", "vhost": "/",
		"queues": [{"name": "work", "type": "classic", "durable": true, "arguments": {"x-max-priority": 10}},
			{"name": "extra", "type": "classic", "durable": false, "arguments": {}}]}}}`

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/topology/diff", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("diff json: %d %s", rec.Code, rec.Body.String())
	}
	var report struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Summary struct {
			Identical bool `json:"identical"`
			Added     int  `json:"added"`
			Changed   int  `json:"changed"`
		} `json:"summary"`
		Queues []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"queues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.From != "dev" || report.To != "prod" {
		t.Fatalf("bad sources: %+v", report)
	}
	if report.Summary.Added != 1 || report.Summary.Changed != 1 || report.Summary.Identical {
		t.Fatalf("bad summary: %+v", report.Summary)
	}
	if len(report.Queues) != 2 {
		t.Fatalf("want 2 queue changes: %+v", report.Queues)
	}

	// text format
	textBody := strings.TrimSuffix(body, "}") + `,"format":"text"}`
	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/topology/diff", strings.NewReader(textBody)))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("diff text: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "+ extra") || !strings.Contains(rec.Body.String(), "summary: 2 difference(s)") {
		t.Fatalf("text report: %s", rec.Body.String())
	}
}

func TestTopologyDiffBadSide(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/topology/diff",
		strings.NewReader(`{"from": {"snapshot": {"format": "other", "version": 9}}, "to": {"snapshot": {"format": "other", "version": 9}}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/topology/diff",
		strings.NewReader(`{"from": {"conn": "ghost", "vhost": "/"}, "to": {"conn": "test", "vhost": "/"}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown conn, got %d", rec.Code)
	}
}

func TestPeekJourney(t *testing.T) {
	msgs := `[
		{"payload_bytes":12,"payload":"hello world!","payload_encoding":"string","redelivered":true,
		 "exchange":"work","routing_key":"jobs","message_count":3,
		 "properties":{"content_type":"application/json",
			"headers":{
				"x-death":[
					{"queue":"dlq","reason":"rejected","count":1,"time":"2026-10-25T10:00:00Z","exchange":"work","routing-keys":["jobs"]},
					{"queue":"retry","reason":"expired","count":2,"time":"2026-10-25T09:00:00Z","exchange":"retry","routing-keys":["jobs"]},
					{"queue":"work","reason":"rejected","count":1,"time":"2026-10-25T08:00:00Z","exchange":"main","routing-keys":["jobs"]}
				],
				"x-first-death":{"queue":"work","exchange":"main","routing-keys":["jobs"],"reason":"rejected"}
			}}}
	]`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/queues/%2F/dlq/get" {
			_, _ = w.Write([]byte(msgs))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	body := `{"conn":"test","vhost":"/","name":"dlq","count":1}`
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/peek", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d %s", rec.Code, rec.Body.String())
	}
	var out []struct {
		Exchange string `json:"exchange"`
		Journey  *struct {
			OriginQueue    string   `json:"origin_queue"`
			OriginExchange string   `json:"origin_exchange"`
			OriginKeys     []string `json:"origin_routing_keys"`
			CurrentQueue   string   `json:"current_queue"`
			Hops           []struct {
				Queue  string `json:"queue"`
				Reason string `json:"reason"`
				Count  int64  `json:"count"`
			} `json:"hops"`
			TotalDeaths int64 `json:"total_deaths"`
			Retries     int64 `json:"retries"`
			Ladders     []struct {
				Queue    string   `json:"queue"`
				Attempts int64    `json:"attempts"`
				Reasons  []string `json:"reasons"`
				Cycle    bool     `json:"cycle"`
			} `json:"ladders"`
			LastDeathTime string  `json:"last_death_time"`
			AgeSec        float64 `json:"age_sec"`
		} `json:"journey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 1 || out[0].Journey == nil {
		t.Fatalf("want one message with journey, got %s", rec.Body.String())
	}
	j := out[0].Journey
	if j.OriginQueue != "work" || j.OriginExchange != "main" {
		t.Fatalf("origin: %+v", j)
	}
	if len(j.OriginKeys) != 1 || j.OriginKeys[0] != "jobs" {
		t.Fatalf("origin keys: %v", j.OriginKeys)
	}
	if j.CurrentQueue != "dlq" {
		t.Fatalf("current queue: %q", j.CurrentQueue)
	}
	if len(j.Hops) != 3 || j.Hops[0].Queue != "work" || j.Hops[2].Queue != "dlq" {
		t.Fatalf("hops: %+v", j.Hops)
	}
	if j.TotalDeaths != 4 || j.Retries != 1 {
		t.Fatalf("totals: deaths=%d retries=%d", j.TotalDeaths, j.Retries)
	}
	if len(j.Ladders) != 1 || j.Ladders[0].Queue != "retry" || j.Ladders[0].Attempts != 2 || !j.Ladders[0].Cycle {
		t.Fatalf("ladders: %+v", j.Ladders)
	}
	if j.LastDeathTime == "" || j.AgeSec <= 0 {
		t.Fatalf("last death: %q age=%v", j.LastDeathTime, j.AgeSec)
	}
}

func TestPeekNoJourneyField(t *testing.T) {
	msgs := `[{"payload_bytes":5,"payload":"hello","payload_encoding":"string","redelivered":false,
		"exchange":"","routing_key":"jobs","message_count":1,"properties":{}}]`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/queues/%2F/work/get" {
			_, _ = w.Write([]byte(msgs))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()
	srv := New(testConfig(t, fake.URL, false))

	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/peek",
		strings.NewReader(`{"conn":"test","vhost":"/","name":"work","count":1}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"journey"`) {
		t.Fatalf("journey must be omitted for non dead-lettered messages: %s", rec.Body.String())
	}
}
