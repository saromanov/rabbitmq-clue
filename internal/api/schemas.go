package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
	"github.com/saromanov/rabbitmq-clue/internal/schema"
)

const peekMaxTruncate = 1024 * 1024

// compileSchemaRequest asks for an in-memory compilation without saving.
type compileSchemaRequest struct {
	Files       map[string]string `json:"files"`
	MessageType string            `json:"message_type"`
}

// createSchemaRequest saves a compiled schema with exchange/queue bindings.
type createSchemaRequest struct {
	Name        string            `json:"name"`
	MessageType string            `json:"message_type"`
	Bindings    []schema.Binding  `json:"bindings"`
	Files       map[string]string `json:"files"`
}

type publishProtoRequest struct {
	Conn              string            `json:"conn"`
	VHost             string            `json:"vhost"`
	SchemaID          string            `json:"schema_id"`
	Files             map[string]string `json:"files"`
	MessageType       string            `json:"message_type"`
	Payload           json.RawMessage   `json:"payload"`
	Encoding          string            `json:"encoding"`
	WrapAny           bool              `json:"wrap_any"`
	JSONUseProtoNames bool              `json:"json_proto_names"`
	ContentType       string            `json:"content_type"`
	Exchange          string            `json:"exchange"`
	RoutingKey        string            `json:"routing_key"`
	Headers           map[string]any    `json:"headers"`
	DeliveryMode      uint8             `json:"delivery_mode"`
	Priority          uint8             `json:"priority"`
}

type peekedMessage struct {
	mgmt.Message
	Decoded     string `json:"decoded,omitempty"`
	DecodedType string `json:"decoded_type,omitempty"`
	Unwrapped   bool   `json:"unwrapped,omitempty"`
	DecodeError string `json:"decode_error,omitempty"`
}

// compiledSchema loads schema files (from the store or inline) and compiles them.
func (s *Server) compiledSchema(id string, inline map[string]string) (*schema.Compiled, error) {
	var files map[string]string
	switch {
	case id != "" && inline != nil:
		return nil, fmt.Errorf("pass either schema_id or files, not both")
	case id != "":
		var err error
		files, err = s.store.Files(id)
		if err != nil {
			return nil, err
		}
	case len(inline) > 0:
		files = inline
	default:
		return nil, fmt.Errorf("schema_id or files required")
	}
	return schema.Compile(files)
}

func (s *Server) handleCompileSchema(w http.ResponseWriter, r *http.Request) {
	var req compileSchemaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	c, err := schema.Compile(req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]any{"types": c.Types()}
	if req.MessageType != "" {
		if !c.HasType(req.MessageType) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown message type %q in schema", req.MessageType))
			return
		}
		skel, fields, err := c.Skeleton(req.MessageType)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp["skeleton"] = skel
		resp["fields"] = fields
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListSchemas(w http.ResponseWriter, r *http.Request) {
	metas, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	exchange, queue := r.URL.Query().Get("exchange"), r.URL.Query().Get("queue")
	out := make([]schema.SchemaMeta, 0, len(metas))
	for _, m := range metas {
		if exchange != "" || queue != "" {
			if !hasBinding(m.Bindings, schema.BindingExchange, exchange) && !hasBinding(m.Bindings, schema.BindingQueue, queue) {
				continue
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func hasBinding(bindings []schema.Binding, kind, name string) bool {
	if name == "" {
		return false
	}
	for _, b := range bindings {
		if b.Kind == kind && b.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	meta, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	files, err := s.store.Files(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           meta.ID,
		"name":         meta.Name,
		"message_type": meta.MessageType,
		"bindings":     meta.Bindings,
		"files":        files,
		"created_at":   meta.CreatedAt,
		"updated_at":   meta.UpdatedAt,
	})
}

func (s *Server) handleCreateSchema(w http.ResponseWriter, r *http.Request) {
	var req createSchemaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.MessageType == "" {
		writeError(w, http.StatusBadRequest, "message_type is required")
		return
	}
	c, err := schema.Compile(req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !c.HasType(req.MessageType) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown message type %q in schema", req.MessageType))
		return
	}
	meta, err := s.store.Create(schema.SchemaMeta{
		Name:        req.Name,
		MessageType: req.MessageType,
		Bindings:    req.Bindings,
	}, req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

func (s *Server) handleUpdateSchema(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req createSchemaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.Files != nil {
		c, err := schema.Compile(req.Files)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.MessageType != "" && !c.HasType(req.MessageType) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown message type %q in schema", req.MessageType))
			return
		}
	}
	meta, err := s.store.Update(id, schema.SchemaMeta{
		Name:        req.Name,
		MessageType: req.MessageType,
		Bindings:    req.Bindings,
	}, req.Files)
	if err != nil {
		writeError(w, apiErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleDeleteSchema(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(chi.URLParam(r, "id")); err != nil {
		writeError(w, apiErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func apiErrorStatus(err error) int {
	if err == schema.ErrNotFound {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (s *Server) handlePublishProto(w http.ResponseWriter, r *http.Request) {
	var req publishProtoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	conn, err := s.connection(req.Conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.compiledSchema(req.SchemaID, req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.MessageType == "" && req.SchemaID != "" {
		meta, err := s.store.Get(req.SchemaID)
		if err != nil {
			writeError(w, apiErrorStatus(err), err.Error())
			return
		}
		req.MessageType = meta.MessageType
	}
	if len(req.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}
	res, err := c.Encode(req.MessageType, req.Payload, schema.EncodeOpts{
		Encoding:          req.Encoding,
		WrapAny:           req.WrapAny,
		JSONUseProtoNames: req.JSONUseProtoNames,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	contentType := res.ContentType
	if req.ContentType != "" {
		contentType = req.ContentType
	}
	session, err := amqpop.Dial(conn.AMQPURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := session.Publish(ctx, amqpop.Publishing{
		Exchange:     req.Exchange,
		RoutingKey:   req.RoutingKey,
		Body:         res.Body,
		ContentType:  contentType,
		Headers:      req.Headers,
		DeliveryMode: req.DeliveryMode,
		Priority:     req.Priority,
	}, true); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// peekWithSchema wraps peeked messages with schema decoding.
func peekWithSchema(c *schema.Compiled, messageType string, msgs []mgmt.Message, req peekRequest) []peekedMessage {
	out := make([]peekedMessage, 0, len(msgs))
	for i := range msgs {
		pm := peekedMessage{Message: msgs[i]}
		raw, err := peekPayloadBytes(msgs[i])
		if err != nil {
			pm.DecodeError = err.Error()
			out = append(out, pm)
			continue
		}
		opts := schema.DecodeOpts{
			PayloadJSON:     req.PayloadJSON || strings.Contains(msgContentType(msgs[i]), "json"),
			UseProtoNames:   req.UseProtoNames,
			EmitUnpopulated: req.EmitUnpopulated,
		}
		decoded, derr := c.Decode(messageType, raw, opts)
		if derr != nil {
			pm.DecodeError = derr.Error()
			out = append(out, pm)
			continue
		}
		pm.Decoded = string(decoded.Decoded)
		pm.DecodedType = decoded.DecodedType
		pm.Unwrapped = decoded.Unwrapped
		out = append(out, pm)
	}
	return out
}

func peekPayloadBytes(m mgmt.Message) ([]byte, error) {
	if strings.EqualFold(m.PayloadEncoding, "base64") {
		return base64.StdEncoding.DecodeString(m.Payload)
	}
	return []byte(m.Payload), nil
}

func msgContentType(m mgmt.Message) string {
	if v, ok := m.Properties["content_type"].(string); ok {
		return v
	}
	return ""
}
