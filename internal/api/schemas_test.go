package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saromanov/rabbitmq-clue/internal/schema"
)

const schemaFixtureProto = `syntax = "proto3";
package clue.api.test.v1;
import "google/protobuf/any.proto";
message Address { string city = 1; string street = 2; }
message AnyEnvelope { google.protobuf.Any payload = 1; }
`

func fixtureCompiled(t *testing.T) *schema.Compiled {
	t.Helper()
	c, err := schema.Compile(map[string]string{"test.proto": schemaFixtureProto})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	return c
}

func saveFixtureSchema(t *testing.T, srv *Server) schema.SchemaMeta {
	t.Helper()
	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":         "addresses",
		"message_type": "clue.api.test.v1.Address",
		"bindings":     []map[string]any{{"kind": "exchange", "name": "orders"}},
		"files":        map[string]string{"test.proto": schemaFixtureProto},
	}
	raw, _ := json.Marshal(body)
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schemas", strings.NewReader(string(raw))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schema: %d %s", rec.Code, rec.Body.String())
	}
	var meta schema.SchemaMeta
	_ = json.Unmarshal(rec.Body.Bytes(), &meta)
	return meta
}

func TestCompileSchemaEndpoint(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	body := `{"files":{"test.proto":` + jsonQuote(schemaFixtureProto) + `},"message_type":"clue.api.test.v1.Address"}`
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schemas/compile", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("compile: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	types, ok := resp["types"].([]any)
	if !ok || len(types) != 2 {
		t.Fatalf("types = %v", resp["types"])
	}
	if resp["skeleton"] == nil || resp["fields"] == nil {
		t.Fatalf("missing skeleton/fields: %v", resp)
	}
	skel, ok := resp["skeleton"].(string)
	if !ok || !strings.Contains(skel, `"city": ""`) {
		t.Errorf("skeleton = %v", resp["skeleton"])
	}
}

func jsonQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func TestCompileSchemaBadProto(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schemas/compile", strings.NewReader(`{"files":{"a.proto":"garbage"}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSchemaCRUDLifecycle(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	h := srv.Handler(nil)
	meta := saveFixtureSchema(t, srv)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schemas?exchange=orders", nil))
	var list []schema.SchemaMeta
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != meta.ID {
		t.Fatalf("filtered list = %+v", list)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schemas?exchange=other", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("expected empty filtered list, got %+v", list)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schemas/"+meta.ID, nil))
	var full map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	files, ok := full["files"].(map[string]any)
	if !ok || files["test.proto"] != schemaFixtureProto {
		t.Fatalf("files = %v", full["files"])
	}

	rec = httptest.NewRecorder()
	update := `{"bindings":[{"kind":"queue","name":"work"}]}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/schemas/"+meta.ID, strings.NewReader(update)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schemas?queue=work", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("queue filter = %+v", list)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/schemas/"+meta.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/schemas/"+meta.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateSchemaUnknownMessageType(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":         "x",
		"message_type": "no.such.Msg",
		"files":        map[string]string{"test.proto": schemaFixtureProto},
	}
	raw, _ := json.Marshal(body)
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schemas", strings.NewReader(string(raw))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSchemaReadOnlyGuard(t *testing.T) {
	srv := New(testConfig(t, "http://unused", true))
	rec := httptest.NewRecorder()
	body := map[string]any{
		"name":         "x",
		"message_type": "clue.api.test.v1.Address",
		"files":        map[string]string{"test.proto": schemaFixtureProto},
	}
	raw, _ := json.Marshal(body)
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/schemas", strings.NewReader(string(raw))))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestPublishProtoValidation(t *testing.T) {
	srv := New(testConfig(t, "http://unused", false))
	h := srv.Handler(nil)
	meta := saveFixtureSchema(t, srv)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/publish/proto", strings.NewReader(`{"schema_id":"nope","payload":{}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown schema: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/publish/proto", strings.NewReader(
		`{"schema_id":"`+meta.ID+`","payload":{"city":123}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad payload type: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPeekWithSchema(t *testing.T) {
	c := fixtureCompiled(t)
	res, err := c.Encode("clue.api.test.v1.Address", []byte(`{"city":"Kazan"}`), schema.EncodeOpts{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(res.Body)

	anyRes, err := c.Encode("clue.api.test.v1.Address", []byte(`{"city":"SPb"}`), schema.EncodeOpts{WrapAny: true})
	if err != nil {
		t.Fatalf("encode any: %v", err)
	}
	anyB64 := base64.StdEncoding.EncodeToString(anyRes.Body)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/queues/%2F/work/get" {
			_, _ = w.Write([]byte(`[
				{"payload_bytes":10,"payload":"` + b64 + `","payload_encoding":"base64","redelivered":false,"exchange":"orders","routing_key":"k","message_count":2,"properties":{"content_type":"application/x-protobuf"}},
				{"payload_bytes":10,"payload":"` + anyB64 + `","payload_encoding":"base64","redelivered":true,"exchange":"orders","routing_key":"k","message_count":1,"properties":{}}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()

	srv := New(testConfig(t, fake.URL, false))
	meta := saveFixtureSchema(t, srv)

	rec := httptest.NewRecorder()
	body := `{"name":"work","vhost":"/","schema_id":"` + meta.ID + `"}`
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/peek", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d %s", rec.Code, rec.Body.String())
	}
	var msgs []peekedMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("msgs = %d", len(msgs))
	}
	if msgs[0].DecodeError != "" || msgs[0].Unwrapped {
		t.Errorf("first msg: %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Decoded, `"city": "Kazan"`) {
		t.Errorf("decoded = %q", msgs[0].Decoded)
	}
	if msgs[0].DecodedType != "clue.api.test.v1.Address" {
		t.Errorf("decoded type = %q", msgs[0].DecodedType)
	}
	if !msgs[1].Unwrapped || !strings.Contains(msgs[1].Decoded, `"city": "SPb"`) {
		t.Errorf("second msg: unwrapped=%v decoded=%q err=%q", msgs[1].Unwrapped, msgs[1].Decoded, msgs[1].DecodeError)
	}
}

func TestPeekWithoutSchemaUnchanged(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/api/queues/%2F/work/get" {
			_, _ = w.Write([]byte(`[{"payload_bytes":4,"payload":"abcd","payload_encoding":"string","exchange":"e","routing_key":"k","message_count":1,"properties":{}}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()

	srv := New(testConfig(t, fake.URL, false))
	rec := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/queues/peek", strings.NewReader(`{"name":"work","vhost":"/"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "decoded") {
		t.Errorf("expected raw messages, got %s", rec.Body.String())
	}
}
