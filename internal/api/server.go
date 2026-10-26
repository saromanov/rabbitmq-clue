package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
	"github.com/saromanov/rabbitmq-clue/internal/config"
	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
	"github.com/saromanov/rabbitmq-clue/internal/replay"
	"github.com/saromanov/rabbitmq-clue/internal/schema"
	"github.com/saromanov/rabbitmq-clue/internal/topology"
	"github.com/saromanov/rabbitmq-clue/internal/version"
)

// Server serves the REST API and the embedded SPA.
type Server struct {
	cfg      *config.Config
	engine   *replay.Engine
	store    *schema.Store
	mgmtOnce sync.Once
	mgmts    map[string]*mgmt.Client
}

// New builds the API server.
func New(cfg *config.Config) *Server {
	return &Server{
		cfg:    cfg,
		engine: replay.NewEngine(),
		store:  schema.NewStore(cfg.SchemasDir),
		mgmts:  map[string]*mgmt.Client{},
	}
}

// Handler assembles all routes.
func (s *Server) Handler(dist fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/api/health", s.handleHealth)
	r.Get("/api/version", s.handleVersion)
	r.Get("/api/connections", s.handleConnections)
	r.Get("/api/overview", s.handleOverview)
	r.Get("/api/vhosts", s.handleVHosts)
	r.Get("/api/queues", s.handleQueues)
	r.Get("/api/queue", s.handleQueue)
	r.Post("/api/queues/peek", s.handlePeek)
	r.Post("/api/queues/ages", s.handleAges)
	r.Post("/api/queues/purge", s.guardMutation(s.handlePurge))
	r.Post("/api/replay", s.guardMutation(s.handleReplay))
	r.Get("/api/replay/jobs", s.handleReplayJobs)
	r.Get("/api/replay/jobs/{id}", s.handleReplayJob)
	r.Post("/api/replay/jobs/{id}/cancel", s.handleReplayCancel)
	r.Post("/api/publish", s.guardMutation(s.handlePublish))
	r.Post("/api/publish/proto", s.guardMutation(s.handlePublishProto))
	r.Get("/api/schemas", s.handleListSchemas)
	r.Post("/api/schemas/compile", s.handleCompileSchema)
	r.Post("/api/schemas", s.guardMutation(s.handleCreateSchema))
	r.Get("/api/schemas/{id}", s.handleGetSchema)
	r.Put("/api/schemas/{id}", s.guardMutation(s.handleUpdateSchema))
	r.Delete("/api/schemas/{id}", s.guardMutation(s.handleDeleteSchema))
	r.Get("/api/exchanges", s.handleExchanges)
	r.Get("/api/bindings", s.handleBindings)
	r.Get("/api/topology/snapshot", s.handleTopologySnapshot)
	r.Post("/api/topology/diff", s.handleTopologyDiff)
	r.Get("/api/events", s.handleEvents)

	if dist != nil {
		r.Handle("/*", spaHandler(dist))
	}
	return r
}

// guardMutation rejects write operations in read-only mode.
func (s *Server) guardMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ReadOnly {
			writeError(w, http.StatusForbidden, "server runs in read-only mode")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// connection resolves the named connection from query/body, defaulting
// to the first configured connection.
func (s *Server) connection(name string) (*config.Connection, error) {
	for i := range s.cfg.Connections {
		if s.cfg.Connections[i].Name == name {
			return &s.cfg.Connections[i], nil
		}
	}
	if name == "" && len(s.cfg.Connections) > 0 {
		return &s.cfg.Connections[0], nil
	}
	return nil, fmt.Errorf("unknown connection %q", name)
}

func (s *Server) mgmtClient(conn *config.Connection) *mgmt.Client {
	s.mgmtOnce.Do(func() {})
	if c, ok := s.mgmts[conn.Name]; ok {
		return c
	}
	c := mgmt.New(conn.MgmtURL, conn.Username, conn.Password)
	s.mgmts[conn.Name] = c
	return c
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Info())
}

func (s *Server) handleConnections(w http.ResponseWriter, _ *http.Request) {
	type connInfo struct {
		Name    string `json:"name"`
		MgmtURL string `json:"mgmt_url"`
		Default bool   `json:"default"`
	}
	out := make([]connInfo, 0, len(s.cfg.Connections))
	for i := range s.cfg.Connections {
		c := s.cfg.Connections[i]
		out = append(out, connInfo{Name: c.Name, MgmtURL: c.MgmtURL, Default: i == 0})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	conn, err := s.connection(r.URL.Query().Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	o, err := s.mgmtClient(conn).Overview(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handleVHosts(w http.ResponseWriter, r *http.Request) {
	conn, err := s.connection(r.URL.Query().Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.mgmtClient(conn).VHosts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleQueues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	queues, err := s.mgmtClient(conn).Queues(r.Context(), q.Get("vhost"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, queues)
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	queue, err := s.mgmtClient(conn).Queue(r.Context(), q.Get("vhost"), q.Get("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, queue)
}

func (s *Server) handleExchanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.mgmtClient(conn).Exchanges(r.Context(), q.Get("vhost"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleBindings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.mgmtClient(conn).Bindings(r.Context(), q.Get("vhost"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// peekRequest targets one queue for a bounded, requeueing peek.
// When SchemaID is set, payloads are additionally decoded as protobuf
// messages of that saved schema.
type peekRequest struct {
	Conn            string `json:"conn"`
	VHost           string `json:"vhost"`
	Name            string `json:"name"`
	Count           int    `json:"count"`
	Truncate        int    `json:"truncate"`
	SchemaID        string `json:"schema_id"`
	PayloadJSON     bool   `json:"payload_json"`
	UseProtoNames   bool   `json:"use_proto_names"`
	EmitUnpopulated bool   `json:"emit_unpopulated"`
}

func (s *Server) handlePeek(w http.ResponseWriter, r *http.Request) {
	var req peekRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.Count <= 0 || req.Count > 100 {
		req.Count = 20
	}
	truncate := 256 * 1024
	if req.Truncate > 0 {
		truncate = req.Truncate
		if truncate > peekMaxTruncate {
			truncate = peekMaxTruncate
		}
	}
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	msgs, err := s.mgmtClient(conn).Peek(r.Context(), req.VHost, req.Name, req.Count, truncate)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if req.SchemaID == "" {
		writeJSON(w, http.StatusOK, msgs)
		return
	}
	c, err := s.compiledSchema(req.SchemaID, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	meta, err := s.store.Get(req.SchemaID)
	if err != nil {
		writeError(w, apiErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, peekWithSchema(c, meta.MessageType, msgs, req))
}

// agesRequest estimates the age of the head message for each named queue.
type agesRequest struct {
	Conn  string   `json:"conn"`
	VHost string   `json:"vhost"`
	Names []string `json:"names"`
}

type queueAge struct {
	Name     string     `json:"name"`
	AgeSec   float64    `json:"age_sec,omitempty"`
	HeadTime *time.Time `json:"head_time,omitempty"`
	Source   string     `json:"source,omitempty"`
}

func (s *Server) handleAges(w http.ResponseWriter, r *http.Request) {
	var req agesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := s.mgmtClient(conn)
	now := time.Now()
	out := make([]queueAge, 0, len(req.Names))
	for _, name := range req.Names {
		age := queueAge{Name: name}
		msgs, err := client.Peek(r.Context(), req.VHost, name, 1, 64*1024)
		if err == nil && len(msgs) > 0 {
			if t, ok := msgs[0].HeadTime(); ok {
				age.HeadTime = &t
				age.AgeSec = now.Sub(t).Seconds()
				age.Source = ageSource(msgs[0])
			}
		}
		out = append(out, age)
	}
	writeJSON(w, http.StatusOK, out)
}

func ageSource(m mgmt.Message) string {
	if _, ok := m.Properties["timestamp"]; ok {
		return "timestamp"
	}
	if headers, ok := m.Properties["headers"].(map[string]any); ok {
		if _, ok := headers["x-death"]; ok {
			return "x-death"
		}
	}
	return ""
}

type purgeRequest struct {
	Conn  string `json:"conn"`
	VHost string `json:"vhost"`
	Name  string `json:"name"`
}

func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	var req purgeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.mgmtClient(conn).Purge(r.Context(), req.VHost, req.Name); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

type replayRequest struct {
	Conn string `json:"conn"`
	replay.Spec
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	var req replayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.engine.Start(conn.AMQPURL, req.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": id})
}

func (s *Server) handleReplayJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.List())
}

func (s *Server) handleReplayJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.engine.Get(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleReplayCancel(w http.ResponseWriter, r *http.Request) {
	if !s.engine.Cancel(chi.URLParam(r, "id")) {
		writeError(w, http.StatusNotFound, "job not found or not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// publishRequest mirrors amqpop.Publishing with connection context.
// Body is overridden as a string because JSON clients send payloads as
// text, not base64.
type publishRequest struct {
	Conn  string `json:"conn"`
	VHost string `json:"vhost"`
	Body  string `json:"body"`
	amqpop.Publishing
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	var req publishRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	req.Publishing.Body = []byte(req.Body)
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := amqpop.Dial(conn.AMQPURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := session.Publish(ctx, req.Publishing, true); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// handleTopologySnapshot exports the topology of one vhost as a
// deterministic JSON snapshot.
func (s *Server) handleTopologySnapshot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snap, err := topology.Capture(r.Context(), conn.Name, q.Get("vhost"), s.mgmtClient(conn))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// topologySide names one side of a diff: either a live broker (connection
// plus vhost) or an inline snapshot document.
type topologySide struct {
	Conn     string          `json:"conn"`
	VHost    string          `json:"vhost"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// topologyDiffRequest compares two sides; format selects the response
// encoding ("json", the default, or "text").
type topologyDiffRequest struct {
	From   topologySide `json:"from"`
	To     topologySide `json:"to"`
	Format string       `json:"format"`
}

// handleTopologyDiff diffs two topologies: both sides can be live
// brokers or previously exported snapshots, mixed freely.
func (s *Server) handleTopologyDiff(w http.ResponseWriter, r *http.Request) {
	var req topologyDiffRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	from, err := s.resolveTopologySide(r.Context(), req.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, "from side: "+err.Error())
		return
	}
	to, err := s.resolveTopologySide(r.Context(), req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, "to side: "+err.Error())
		return
	}
	report := topology.Diff(from, to)
	if req.Format == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(topology.RenderText(report)))
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// resolveTopologySide loads one diff side from an inline snapshot or
// captures it live from a configured connection.
func (s *Server) resolveTopologySide(ctx context.Context, side topologySide) (*topology.Snapshot, error) {
	if len(side.Snapshot) > 0 {
		return topology.Load(side.Snapshot)
	}
	conn, err := s.connection(side.Conn)
	if err != nil {
		return nil, err
	}
	return topology.Capture(ctx, conn.Name, side.VHost, s.mgmtClient(conn))
}

// handleEvents streams per-queue stats snapshots over SSE for live views.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conn, err := s.connection(q.Get("conn"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	interval := 2 * time.Second
	if v, err := strconv.Atoi(q.Get("interval_ms")); err == nil && v >= 500 && v <= 15000 {
		interval = time.Duration(v) * time.Millisecond
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	client := s.mgmtClient(conn)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			queues, err := client.Queues(r.Context(), q.Get("vhost"))
			if err != nil {
				continue
			}
			payload, err := json.Marshal(queues)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: queues\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// spaHandler serves the embedded SPA with index.html fallback for
// client-side routes.
func spaHandler(dist fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(dist))
	index, indexErr := fs.ReadFile(dist, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			path = "index.html"
		}
		if path == "index.html" {
			if indexErr != nil {
				http.Error(w, "SPA is not built", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		r.URL.Path = "/" + path
		fileServer.ServeHTTP(w, r)
	}
}
