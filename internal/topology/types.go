// Package topology captures RabbitMQ topology snapshots (queues,
// exchanges, bindings, policies) and diffs them between environments,
// e.g. dev versus production.
package topology

import "time"

// Format identifies the snapshot document; Version is its schema version.
const (
	Format  = "rabbitmq-clue.topology"
	Version = 1
)

// Snapshot is a vhost-scoped topology export. Specs keep only
// topology-relevant attributes, sorted deterministically so the same
// broker state always produces the same document — snapshots are meant
// to be stored, shared and diffed by tools.
type Snapshot struct {
	Format    string         `json:"format"`
	Version   int            `json:"version"`
	TakenAt   time.Time      `json:"taken_at"`
	Source    string         `json:"source"`
	VHost     string         `json:"vhost"`
	Queues    []QueueSpec    `json:"queues"`
	Exchanges []ExchangeSpec `json:"exchanges"`
	Bindings  []BindingSpec  `json:"bindings"`
	Policies  []PolicySpec   `json:"policies"`
}

// QueueSpec is the topology-relevant subset of a queue declaration.
type QueueSpec struct {
	Name       string         `json:"name"`
	Type       string         `json:"type,omitempty"`
	Durable    bool           `json:"durable"`
	AutoDelete bool           `json:"auto_delete"`
	Exclusive  bool           `json:"exclusive"`
	Arguments  map[string]any `json:"arguments"`
}

// ExchangeSpec is the topology-relevant subset of an exchange declaration.
type ExchangeSpec struct {
	Name       string         `json:"name"`
	Type       string         `json:"type,omitempty"`
	Durable    bool           `json:"durable"`
	AutoDelete bool           `json:"auto_delete"`
	Internal   bool           `json:"internal"`
	Arguments  map[string]any `json:"arguments"`
}

// BindingSpec is the topology-relevant subset of a binding. The vhost is
// carried by the snapshot header, not by each binding.
type BindingSpec struct {
	Source          string         `json:"source"`
	Destination     string         `json:"destination"`
	DestinationType string         `json:"destination_type"`
	RoutingKey      string         `json:"routing_key"`
	Arguments       map[string]any `json:"arguments"`
}

// PolicySpec is the topology-relevant subset of a policy.
type PolicySpec struct {
	Name       string         `json:"name"`
	ApplyTo    string         `json:"apply_to,omitempty"`
	Pattern    string         `json:"pattern,omitempty"`
	Priority   int            `json:"priority,omitempty"`
	Definition map[string]any `json:"definition"`
}
