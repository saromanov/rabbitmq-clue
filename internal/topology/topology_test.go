package topology

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
)

func TestDiffIdentical(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	b := testSnapshot(t, snapshotDocA)
	report := Diff(a, b)
	if !report.Summary.Identical {
		t.Fatalf("want identical, got %+v", report.Summary)
	}
	if report.Queues != nil || report.Exchanges != nil || report.Bindings != nil || report.Policies != nil {
		t.Fatalf("identical report must have nil sections: %+v", report)
	}
	if report.Summary.Added != 0 || report.Summary.Removed != 0 || report.Summary.Changed != 0 {
		t.Fatalf("unexpected counts: %+v", report.Summary)
	}
}

func TestDiffQueueLifecycle(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	b := testSnapshot(t, snapshotDocB)
	report := Diff(a, b)

	// added: orders.retry; removed: legacy.jobs; changed: orders.main (x-max-length dropped)
	var added, removed, changed *QueueChange
	for i := range report.Queues {
		switch report.Queues[i].Name {
		case "orders.retry":
			added = &report.Queues[i]
		case "legacy.jobs":
			removed = &report.Queues[i]
		case "orders.main":
			changed = &report.Queues[i]
		}
	}
	if added == nil || removed == nil || changed == nil {
		t.Fatalf("missing changes: %+v", report.Queues)
	}
	if added.Kind != Added || added.To == nil {
		t.Fatalf("bad added change: %+v", added)
	}
	if removed.Kind != Removed || removed.From == nil {
		t.Fatalf("bad removed change: %+v", removed)
	}
	if changed.Kind != Changed {
		t.Fatalf("bad changed kind: %+v", changed)
	}
	fields := map[string]FieldDiff{}
	for _, f := range changed.Fields {
		fields[f.Field] = f
	}
	arg, ok := fields["arguments.x-max-length"]
	if !ok {
		t.Fatalf("want x-max-length diff: %+v", fields)
	}
	if arg.From != float64(10000) || arg.To != nil {
		t.Fatalf("x-max-length diff wrong: %+v", arg)
	}
	if _, ok := fields["durable"]; ok {
		t.Fatalf("durable is unchanged in fixtures: %+v", fields)
	}
	// unchanged attribute must not appear
	if _, ok := fields["type"]; ok {
		t.Fatalf("type diff should not exist: %+v", fields)
	}
}

func TestDiffBindingsAndPolicies(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	b := testSnapshot(t, snapshotDocB)
	report := Diff(a, b)

	if len(report.Bindings) != 2 {
		t.Fatalf("want 2 binding changes, got %+v", report.Bindings)
	}
	// one added (orders.main -> orders.retry [retry]), one changed args on existing
	var argsChanged bool
	for _, c := range report.Bindings {
		if c.Kind == Changed {
			argsChanged = true
			if len(c.Fields) != 1 || c.Fields[0].Field != "arguments.x-match" {
				t.Fatalf("binding args diff wrong: %+v", c.Fields)
			}
		}
	}
	if !argsChanged {
		t.Fatalf("want a binding with changed arguments: %+v", report.Bindings)
	}

	if len(report.Policies) != 2 {
		t.Fatalf("want 2 policy changes, got %+v", report.Policies)
	}
	var dlq *PolicyChange
	for i := range report.Policies {
		if report.Policies[i].Name == "dlq-policy" {
			dlq = &report.Policies[i]
		}
	}
	if dlq == nil || dlq.Kind != Changed {
		t.Fatalf("dlq-policy must be changed: %+v", report.Policies)
	}
	if len(dlq.Fields) != 1 || dlq.Fields[0].Field != "definition.ha-mode" {
		t.Fatalf("policy diff wrong: %+v", dlq.Fields)
	}
	if got, _ := renderValue(dlq.Fields[0].From), renderValue(dlq.Fields[0].To); got == "" {
		t.Fatal("renderValue must render")
	}
}

func TestDiffVHostMismatch(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	a.VHost = "/"
	b := testSnapshot(t, snapshotDocA)
	b.VHost = "prod"
	report := Diff(a, b)
	if report.VHost != "/ → prod" {
		t.Fatalf("vhost label: %q", report.VHost)
	}
}

// TestDiffDeterministic feeds the same content in scrambled order and
// expects Load to normalize both sides so the reports match exactly.
func TestDiffDeterministic(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	b := testSnapshot(t, snapshotDocB)
	one := Diff(a, b)

	scrambled := testSnapshot(t, snapshotDocAScrambled)
	two := Diff(scrambled, b)
	if len(one.Queues) != len(two.Queues) || len(one.Bindings) != len(two.Bindings) {
		t.Fatalf("report shape mismatch after reorder")
	}
	for i := range one.Queues {
		if one.Queues[i].Name != two.Queues[i].Name || one.Queues[i].Kind != two.Queues[i].Kind {
			t.Fatalf("report must be name-ordered: %q vs %q", one.Queues[i].Name, two.Queues[i].Name)
		}
	}
	if one.Summary != two.Summary {
		t.Fatalf("summary mismatch: %+v vs %+v", one.Summary, two.Summary)
	}
}

func TestArgsDiffCanonicalJSON(t *testing.T) {
	// Same nested map with different key order must compare equal.
	from := map[string]any{"headers": map[string]any{"a": 1, "b": 2}}
	to := map[string]any{"headers": map[string]any{"b": 2, "a": 1}}
	if fields := argsDiff("arguments", from, to); fields != nil {
		t.Fatalf("same args must not differ: %+v", fields)
	}
	// Numbers as int vs float must compare equal (canonical JSON).
	if fields := argsDiff("arguments", map[string]any{"x": 10}, map[string]any{"x": float64(10)}); fields != nil {
		t.Fatalf("equal numbers must not differ: %+v", fields)
	}
}

func TestCaptureAndLoadRoundTrip(t *testing.T) {
	srv := fakeMgmtServer(t)
	defer srv.Close()
	client := mgmt.New(srv.URL, "u", "p")

	snap, err := Capture(context.Background(), "dev", "/", client)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.Format != Format || snap.Version != Version {
		t.Fatalf("bad header: %+v", snap)
	}
	if snap.Source != "dev" || snap.VHost != "/" {
		t.Fatalf("bad source/vhost: %+v", snap)
	}
	// sorted and normalized
	if !sortOK(t, snap) {
		t.Fatal("snapshot not sorted")
	}
	for _, q := range snap.Queues {
		if q.Arguments == nil {
			t.Fatalf("nil arguments must be normalized: %+v", q)
		}
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, err := Load(data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reloaded := Diff(snap, loaded)
	if !reloaded.Summary.Identical {
		t.Fatalf("round trip must be identical: %+v", reloaded.Summary)
	}
}

func TestLoadRejectsForeignFormat(t *testing.T) {
	if _, err := Load([]byte(`{"format":"other","version":1}`)); err == nil {
		t.Fatal("want error for foreign format")
	}
	if _, err := Load([]byte(`not json`)); err == nil {
		t.Fatal("want error for invalid json")
	}
}

func TestRenderText(t *testing.T) {
	a := testSnapshot(t, snapshotDocA)
	b := testSnapshot(t, snapshotDocB)
	report := Diff(a, b)
	text := RenderText(report)

	for _, want := range []string{
		"topology diff: dev → prod [vhost /]",
		"queues (3):",
		"+ orders.retry",
		"- legacy.jobs",
		"~ orders.main",
		"arguments.x-max-length: 10000 → (absent)",
		"bindings (2):",
		"policies (2):",
		"summary: 7 difference(s) — 3 added, 1 removed, 3 changed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text report missing %q:\n%s", want, text)
		}
	}

	identical := Diff(a, testSnapshot(t, snapshotDocA))
	if text := RenderText(identical); !strings.Contains(text, "\nidentical\n") {
		t.Fatalf("identical text report: %s", text)
	}
}

const snapshotDocA = `{
  "format": "rabbitmq-clue.topology", "version": 1, "source": "dev", "vhost": "/",
  "queues": [
    {"name": "legacy.jobs", "type": "classic", "durable": true, "arguments": {}},
    {"name": "orders.main", "type": "quorum", "durable": true, "arguments": {"x-max-length": 10000}},
    {"name": "orders.events", "type": "classic", "durable": false, "arguments": {}}
  ],
  "exchanges": [
    {"name": "orders", "type": "topic", "durable": true, "arguments": {}}
  ],
  "bindings": [
    {"source": "orders", "destination": "orders.main", "destination_type": "queue", "routing_key": "order.*", "arguments": {"x-match": "any"}},
    {"source": "orders", "destination": "orders.events", "destination_type": "queue", "routing_key": "order.created", "arguments": {}}
  ],
  "policies": [
    {"name": "dlq-policy", "apply_to": "queues", "pattern": "^orders\\.", "definition": {"ha-mode": "all"}}
  ]
}`

// snapshotDocAScrambled has the same content as snapshotDocA in reverse
// order; Load must normalize it into the identical document.
const snapshotDocAScrambled = `{
  "format": "rabbitmq-clue.topology", "version": 1, "source": "dev", "vhost": "/",
  "queues": [
    {"name": "orders.events", "type": "classic", "durable": false, "arguments": {}},
    {"name": "orders.main", "type": "quorum", "durable": true, "arguments": {"x-max-length": 10000}},
    {"name": "legacy.jobs", "type": "classic", "durable": true, "arguments": {}}
  ],
  "exchanges": [
    {"name": "orders", "type": "topic", "durable": true, "arguments": {}}
  ],
  "bindings": [
    {"source": "orders", "destination": "orders.events", "destination_type": "queue", "routing_key": "order.created", "arguments": {}},
    {"source": "orders", "destination": "orders.main", "destination_type": "queue", "routing_key": "order.*", "arguments": {"x-match": "any"}}
  ],
  "policies": [
    {"name": "dlq-policy", "apply_to": "queues", "pattern": "^orders\\.", "definition": {"ha-mode": "all"}}
  ]
}`

const snapshotDocB = `{
  "format": "rabbitmq-clue.topology", "version": 1, "source": "prod", "vhost": "/",
  "queues": [
    {"name": "orders.main", "type": "quorum", "durable": true, "arguments": {}},
    {"name": "orders.retry", "type": "classic", "durable": false, "auto_delete": true, "arguments": {}},
    {"name": "orders.events", "type": "classic", "durable": false, "arguments": {}}
  ],
  "exchanges": [
    {"name": "orders", "type": "topic", "durable": true, "arguments": {}}
  ],
  "bindings": [
    {"source": "orders", "destination": "orders.main", "destination_type": "queue", "routing_key": "order.*", "arguments": {"x-match": "all"}},
    {"source": "orders", "destination": "orders.retry", "destination_type": "queue", "routing_key": "retry", "arguments": {}},
    {"source": "orders", "destination": "orders.events", "destination_type": "queue", "routing_key": "order.created", "arguments": {}}
  ],
  "policies": [
    {"name": "dlq-policy", "apply_to": "queues", "pattern": "^orders\\.", "definition": {"ha-mode": "exactly"}},
    {"name": "ttl-policy", "apply_to": "queues", "pattern": "^legacy", "definition": {"message-ttl": 60000}}
  ]
}`

func testSnapshot(t *testing.T, doc string) *Snapshot {
	t.Helper()
	snap, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("load test snapshot: %v", err)
	}
	return snap
}

func sortOK(t *testing.T, snap *Snapshot) bool {
	t.Helper()
	for i := 1; i < len(snap.Queues); i++ {
		if snap.Queues[i-1].Name > snap.Queues[i].Name {
			return false
		}
	}
	for i := 1; i < len(snap.Exchanges); i++ {
		if snap.Exchanges[i-1].Name > snap.Exchanges[i].Name {
			return false
		}
	}
	for i := 1; i < len(snap.Policies); i++ {
		if snap.Policies[i-1].Name > snap.Policies[i].Name {
			return false
		}
	}
	return true
}

func fakeMgmtServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/queues/%2F":
			_, _ = w.Write([]byte(`[
				{"name":"orders.main","type":"quorum","durable":true,"arguments":{"x-max-length":10000}},
				{"name":"orders.events","durable":false,"exclusive":false,"auto_delete":false,"arguments":null}
			]`))
		case "/api/exchanges/%2F":
			_, _ = w.Write([]byte(`[{"name":"orders","type":"topic","durable":true,"arguments":{}}]`))
		case "/api/bindings/%2F":
			_, _ = w.Write([]byte(`[
				{"source":"orders","destination":"orders.main","destination_type":"queue","routing_key":"order.*","arguments":{"x-match":"any"}},
				{"source":"orders","destination":"orders.events","destination_type":"queue","routing_key":"order.created","arguments":{}}
			]`))
		case "/api/policies/%2F":
			_, _ = w.Write([]byte(`[{"name":"dlq-policy","vhost":"/","apply-to":"queues","pattern":"^orders\\\\.","priority":0,"definition":{"ha-mode":"all"}}]`))
		default:
			t.Fatalf("unexpected mgmt path: %s", r.URL.EscapedPath())
		}
	}))
}
