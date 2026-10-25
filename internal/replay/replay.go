package replay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/saromanov/rabbitmq-clue/internal/amqpop"
)

// Filter selects which messages participate in a replay.
// Substring matches decoded payloads; Header requires an exact
// key/value match against message headers.
type Filter struct {
	Kind   string `json:"kind"` // "substring" | "header"
	Value  string `json:"value"`
	Header string `json:"header,omitempty"`
}

// Spec fully describes one replay run.
type Spec struct {
	VHost          string   `json:"vhost"`
	SourceQueue    string   `json:"source_queue"`
	TargetQueue    string   `json:"target_queue,omitempty"`
	TargetExchange string   `json:"target_exchange,omitempty"`
	TargetKey      string   `json:"target_routing_key,omitempty"`
	Limit          int      `json:"limit"`
	DryRun         bool     `json:"dry_run"`
	StripXDeath    bool     `json:"strip_x_death"`
	Filters        []Filter `json:"filters,omitempty"`
}

// Progress is the mutable state of a running job.
type Progress struct {
	Processed int64 `json:"processed"`
	Succeeded int64 `json:"succeeded"`
	Skipped   int64 `json:"skipped"`
	Failed    int64 `json:"failed"`
}

// Job tracks one replay run from creation to completion.
type Job struct {
	ID        string     `json:"id"`
	Spec      Spec       `json:"spec"`
	Status    string     `json:"status"` // running | completed | cancelled | failed
	Progress  Progress   `json:"progress"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Errors    []string   `json:"errors,omitempty"`
	AMQPURL   string     `json:"-"`
}

func (j *Job) fail(err error) {
	j.Status = "failed"
	j.appendError(err)
	j.markEnded()
}

func (j *Job) appendError(err error) {
	const maxErrors = 50
	if len(j.Errors) < maxErrors {
		j.Errors = append(j.Errors, err.Error())
	}
}

func (j *Job) markEnded() {
	now := time.Now()
	j.EndedAt = &now
}

// Engine creates and tracks replay jobs. Jobs mutate their own Job struct;
// the API layer reads it for polling, guarded by the engine mutex.
type Engine struct {
	mu    sync.RWMutex
	jobs  map[string]*Job
	order []string
}

// NewEngine returns an empty replay engine.
func NewEngine() *Engine {
	return &Engine{jobs: map[string]*Job{}}
}

// Start launches a replay in a background goroutine and returns its id.
func (e *Engine) Start(amqpURL string, spec Spec) (string, error) {
	if spec.SourceQueue == "" {
		return "", fmt.Errorf("source_queue is required")
	}
	if spec.TargetQueue == "" && spec.TargetExchange == "" {
		return "", fmt.Errorf("target_queue or target_exchange is required")
	}
	job := &Job{
		ID:        newID(),
		Spec:      spec,
		Status:    "running",
		StartedAt: time.Now(),
		AMQPURL:   amqpURL,
	}
	e.mu.Lock()
	e.jobs[job.ID] = job
	e.order = append(e.order, job.ID)
	e.mu.Unlock()
	go e.run(job)
	return job.ID, nil
}

// Get returns a copy of the job state.
func (e *Engine) Get(id string) (Job, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	j, ok := e.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// Cancel requests cancellation of a running job.
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.jobs[id]
	if !ok || j.Status != "running" {
		return false
	}
	j.Status = "cancelled"
	return true
}

// List returns all jobs, newest first.
func (e *Engine) List() []Job {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Job, 0, len(e.order))
	for i := len(e.order) - 1; i >= 0; i-- {
		out = append(out, *e.jobs[e.order[i]])
	}
	return out
}

func (e *Engine) running(job *Job) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return job.Status == "running"
}

func (e *Engine) run(job *Job) {
	ctx := context.Background()
	session, err := amqpop.Dial(job.AMQPURL)
	if err != nil {
		e.mu.Lock()
		job.fail(err)
		e.mu.Unlock()
		return
	}
	defer session.Close()

	target := job.Spec.TargetQueue
	var exchange string
	if target == "" {
		exchange = job.Spec.TargetExchange
	}

	// Snapshot of the source queue length when the job starts. Dry-run
	// scans exactly this many messages: it re-publishes every message to
	// the tail (the only way to traverse a queue in AMQP 0-9-1), so a
	// counter is the reliable full-cycle signal.
	srcInfo, err := session.Channel.QueueDeclarePassive(job.Spec.SourceQueue, true, false, false, false, nil)
	if err != nil {
		e.mu.Lock()
		job.fail(fmt.Errorf("source queue %q: %w", job.Spec.SourceQueue, err))
		e.mu.Unlock()
		return
	}
	initialCount := int64(srcInfo.Messages)

	// Fingerprints of non-matching messages that were moved back to the
	// source queue. A skip re-publishes the message to the tail, so seeing
	// the same fingerprint again means all remaining messages were already
	// skipped and the loop must stop.
	seen := map[string]struct{}{}

	for e.running(job) {
		if job.Spec.Limit > 0 && job.Progress.Processed >= int64(job.Spec.Limit) {
			break
		}
		if job.Spec.DryRun && job.Progress.Processed >= initialCount {
			break
		}
		delivery, ok, err := session.GetOne(job.Spec.SourceQueue)
		if err != nil {
			e.mu.Lock()
			job.fail(err)
			e.mu.Unlock()
			return
		}
		if !ok {
			break
		}
		e.mu.Lock()
		job.Progress.Processed++
		e.mu.Unlock()

		isMatch := matches(delivery, job.Spec.Filters)
		if job.Spec.DryRun {
			if isMatch {
				e.mu.Lock()
				job.Progress.Succeeded++
				e.mu.Unlock()
			}
			if err := session.Publish(ctx, toSourcePublishing(delivery, job.Spec.SourceQueue), true); err != nil {
				_ = session.Channel.Nack(delivery.DeliveryTag, false, true)
				e.mu.Lock()
				job.fail(err)
				e.mu.Unlock()
				return
			}
			if err := session.Channel.Ack(delivery.DeliveryTag, false); err != nil {
				e.mu.Lock()
				job.fail(fmt.Errorf("ack after dry-run requeue: %w", err))
				e.mu.Unlock()
				return
			}
			continue
		}

		if !isMatch {
			fp := fingerprint(delivery)
			if _, dup := seen[fp]; dup {
				break
			}
			seen[fp] = struct{}{}
			e.mu.Lock()
			job.Progress.Skipped++
			e.mu.Unlock()
			if err := session.Publish(ctx, toSourcePublishing(delivery, job.Spec.SourceQueue), true); err != nil {
				_ = session.Channel.Nack(delivery.DeliveryTag, false, true)
				e.mu.Lock()
				job.fail(err)
				e.mu.Unlock()
				return
			}
			if err := session.Channel.Ack(delivery.DeliveryTag, false); err != nil {
				e.mu.Lock()
				job.fail(fmt.Errorf("ack after requeue: %w", err))
				e.mu.Unlock()
				return
			}
			continue
		}

		pub := toPublishing(delivery, target, exchange, job.Spec.TargetKey, job.Spec.StripXDeath)
		if err := session.Publish(ctx, pub, true); err != nil {
			_ = session.Channel.Nack(delivery.DeliveryTag, false, true)
			e.mu.Lock()
			job.fail(err)
			e.mu.Unlock()
			return
		}
		if err := session.Channel.Ack(delivery.DeliveryTag, false); err != nil {
			e.mu.Lock()
			job.fail(fmt.Errorf("ack after confirm: %w", err))
			e.mu.Unlock()
			return
		}
		e.mu.Lock()
		job.Progress.Succeeded++
		e.mu.Unlock()
	}

	e.mu.Lock()
	if job.Status == "running" {
		job.Status = "completed"
		job.markEnded()
	}
	e.mu.Unlock()
}

// toPublishing converts an inbound delivery into an outbound publishing,
// preserving all properties and headers. A replay marker is added so
// operators can trace replayed messages, and x-death may be stripped to
// avoid re-dead-letter loops on the target side.
func toPublishing(d amqp091.Delivery, targetQueue, targetExchange, targetKey string, stripXDeath bool) amqpop.Publishing {
	headers := map[string]any{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	if stripXDeath {
		delete(headers, "x-death")
		delete(headers, "x-first-death")
		delete(headers, "x-last-death")
	}
	headers["x-replayed-by"] = "rabbitmq-clue"
	headers["x-replay-time"] = time.Now().UTC().Format(time.RFC3339)

	routingKey := targetKey
	if routingKey == "" {
		routingKey = d.RoutingKey
	}
	if targetQueue != "" {
		routingKey = targetQueue
	}
	return amqpop.Publishing{
		Exchange:      targetExchange,
		RoutingKey:    routingKey,
		Body:          d.Body,
		ContentType:   d.ContentType,
		ContentEnc:    d.ContentEncoding,
		Headers:       headers,
		DeliveryMode:  d.DeliveryMode,
		Priority:      d.Priority,
		Expiration:    d.Expiration,
		CorrelationID: d.CorrelationId,
		ReplyTo:       d.ReplyTo,
		MessageID:     d.MessageId,
		Type:          d.Type,
		AppID:         d.AppId,
		Timestamp:     d.Timestamp,
	}
}

// newID generates an opaque job identifier.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// toSourcePublishing rebuilds a delivery for moving it back to the tail
// of its source queue, preserving all properties and headers verbatim
// and adding no replay markers.
func toSourcePublishing(d amqp091.Delivery, sourceQueue string) amqpop.Publishing {
	headers := map[string]any{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	return amqpop.Publishing{
		Exchange:      "",
		RoutingKey:    sourceQueue,
		Body:          d.Body,
		ContentType:   d.ContentType,
		ContentEnc:    d.ContentEncoding,
		Headers:       headers,
		DeliveryMode:  d.DeliveryMode,
		Priority:      d.Priority,
		Expiration:    d.Expiration,
		CorrelationID: d.CorrelationId,
		ReplyTo:       d.ReplyTo,
		MessageID:     d.MessageId,
		Type:          d.Type,
		AppID:         d.AppId,
		Timestamp:     d.Timestamp,
	}
}

// fingerprint builds a stable identity of a message from its body and
// headers, used to detect that a requeued message came back to the head.
func fingerprint(d amqp091.Delivery) string {
	h := sha256.New()
	h.Write(d.Body)
	keys := make([]string, 0, len(d.Headers))
	for k := range d.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "\x00%s=%v", k, d.Headers[k])
	}
	return string(h.Sum(nil))
}

// matches reports whether a delivery passes every filter.
func matches(d amqp091.Delivery, filters []Filter) bool {
	for _, f := range filters {
		switch f.Kind {
		case "substring":
			if !strings.Contains(string(d.Body), f.Value) {
				return false
			}
		case "header":
			v, ok := d.Headers[f.Header]
			if !ok || fmt.Sprintf("%v", v) != f.Value {
				return false
			}
		}
	}
	return true
}
