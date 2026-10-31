package mgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a typed client for the RabbitMQ management HTTP API.
// It is intentionally tolerant to broker version differences: unknown
// fields are ignored and missing fields default to zero values.
type Client struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

// New creates a management API client.
func New(baseURL, user, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		user:     user,
		password: password,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// APIError is a non-2xx response from the management API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("management API: status %d: %s", e.StatusCode, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// vhostPathSegment encodes a vhost name for use inside an API path.
// VHost names may contain "/" (the default vhost is literally "/").
func vhostPathSegment(vhost string) string {
	return url.PathEscape(vhost)
}

// Overview returns broker overview information.
func (c *Client) Overview(ctx context.Context) (*Overview, error) {
	var o Overview
	if err := c.getJSON(ctx, "/api/overview", &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// VHosts lists virtual hosts visible to the authenticated user.
func (c *Client) VHosts(ctx context.Context) ([]VHost, error) {
	var v []VHost
	if err := c.getJSON(ctx, "/api/vhosts", &v); err != nil {
		return nil, err
	}
	return v, nil
}

// Queues lists queues of a vhost (all queues when vhost is empty).
func (c *Client) Queues(ctx context.Context, vhost string) ([]Queue, error) {
	path := "/api/queues"
	if vhost != "" {
		path = "/api/queues/" + vhostPathSegment(vhost)
	}
	var q []Queue
	if err := c.getJSON(ctx, path, &q); err != nil {
		return nil, err
	}
	for i := range q {
		q[i].VHost = firstNonEmpty(q[i].VHost, vhost)
	}
	return q, nil
}

// Queue returns a single queue by name.
func (c *Client) Queue(ctx context.Context, vhost, name string) (*Queue, error) {
	var q Queue
	path := fmt.Sprintf("/api/queues/%s/%s", vhostPathSegment(vhost), url.PathEscape(name))
	if err := c.getJSON(ctx, path, &q); err != nil {
		return nil, err
	}
	q.VHost = firstNonEmpty(q.VHost, vhost)
	return &q, nil
}

// Exchanges lists exchanges of a vhost (all exchanges when vhost is empty).
func (c *Client) Exchanges(ctx context.Context, vhost string) ([]Exchange, error) {
	path := "/api/exchanges"
	if vhost != "" {
		path = "/api/exchanges/" + vhostPathSegment(vhost)
	}
	var e []Exchange
	if err := c.getJSON(ctx, path, &e); err != nil {
		return nil, err
	}
	for i := range e {
		e[i].VHost = firstNonEmpty(e[i].VHost, vhost)
	}
	return e, nil
}

// Bindings lists bindings of a vhost (all bindings when vhost is empty).
func (c *Client) Bindings(ctx context.Context, vhost string) ([]Binding, error) {
	path := "/api/bindings"
	if vhost != "" {
		path = "/api/bindings/" + vhostPathSegment(vhost)
	}
	var b []Binding
	if err := c.getJSON(ctx, path, &b); err != nil {
		return nil, err
	}
	for i := range b {
		b[i].VHost = firstNonEmpty(b[i].VHost, vhost)
	}
	return b, nil
}

// Policies lists policies of a vhost (all policies when vhost is empty).
func (c *Client) Policies(ctx context.Context, vhost string) ([]Policy, error) {
	path := "/api/policies"
	if vhost != "" {
		path = "/api/policies/" + vhostPathSegment(vhost)
	}
	var p []Policy
	if err := c.getJSON(ctx, path, &p); err != nil {
		return nil, err
	}
	for i := range p {
		p[i].VHost = firstNonEmpty(p[i].VHost, vhost)
	}
	return p, nil
}

// Peek fetches up to count messages from the head of a queue without
// consuming them (ack_requeue_true). This is the same destructive-ish
// operation the stock management UI uses: RabbitMQ re-queues the messages
// and may reorder them. Callers must surface this caveat in the UI.
func (c *Client) Peek(ctx context.Context, vhost, name string, count, truncate int) ([]Message, error) {
	path := fmt.Sprintf("/api/queues/%s/%s/get", vhostPathSegment(vhost), url.PathEscape(name))
	body := PublishMessage{Count: count, Ackmode: AckRequeueTrue, Truncate: truncate, Encoding: "auto"}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	data, err := c.do(ctx, http.MethodPost, path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(raws))
	for _, raw := range raws {
		m, err := decodeMessage(raw)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// Purge removes all messages from a queue.
func (c *Client) Purge(ctx context.Context, vhost, name string) error {
	path := fmt.Sprintf("/api/queues/%s/%s/contents", vhostPathSegment(vhost), url.PathEscape(name))
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
