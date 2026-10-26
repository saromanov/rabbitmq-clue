package topology

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
)

// Capture builds a snapshot of the given vhost from a management API
// client. The result is deterministic: specs are normalized (nil
// arguments become empty maps) and sorted by their identity keys.
func Capture(ctx context.Context, source, vhost string, client *mgmt.Client) (*Snapshot, error) {
	queues, err := client.Queues(ctx, vhost)
	if err != nil {
		return nil, fmt.Errorf("capture queues: %w", err)
	}
	exchanges, err := client.Exchanges(ctx, vhost)
	if err != nil {
		return nil, fmt.Errorf("capture exchanges: %w", err)
	}
	bindings, err := client.Bindings(ctx, vhost)
	if err != nil {
		return nil, fmt.Errorf("capture bindings: %w", err)
	}
	policies, err := client.Policies(ctx, vhost)
	if err != nil {
		return nil, fmt.Errorf("capture policies: %w", err)
	}

	snap := &Snapshot{
		Format:    Format,
		Version:   Version,
		Source:    source,
		VHost:     vhost,
		Queues:    make([]QueueSpec, 0, len(queues)),
		Exchanges: make([]ExchangeSpec, 0, len(exchanges)),
		Bindings:  make([]BindingSpec, 0, len(bindings)),
		Policies:  make([]PolicySpec, 0, len(policies)),
	}
	for _, q := range queues {
		snap.Queues = append(snap.Queues, QueueSpec{
			Name:       q.Name,
			Type:       q.Type,
			Durable:    q.Durable,
			AutoDelete: q.AutoDelete,
			Exclusive:  q.Exclusive,
			Arguments:  normalize(q.Arguments),
		})
	}
	for _, e := range exchanges {
		snap.Exchanges = append(snap.Exchanges, ExchangeSpec{
			Name:       e.Name,
			Type:       e.Type,
			Durable:    e.Durable,
			AutoDelete: e.AutoDelete,
			Internal:   e.Internal,
			Arguments:  normalize(e.Arguments),
		})
	}
	for _, b := range bindings {
		snap.Bindings = append(snap.Bindings, BindingSpec{
			Source:          b.Source,
			Destination:     b.Destination,
			DestinationType: b.DestinationType,
			RoutingKey:      b.RoutingKey,
			Arguments:       normalize(b.Arguments),
		})
	}
	for _, p := range policies {
		snap.Policies = append(snap.Policies, PolicySpec{
			Name:       p.Name,
			ApplyTo:    p.ApplyTo,
			Pattern:    p.Pattern,
			Priority:   p.Priority,
			Definition: normalize(p.Definition),
		})
	}
	sortSnapshot(snap)
	return snap, nil
}

// Load parses a snapshot document produced by Capture and validates its
// format marker.
func Load(data []byte) (*Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if snap.Format != Format || snap.Version != Version {
		return nil, fmt.Errorf("unsupported snapshot format %q version %d (want %q version %d)",
			snap.Format, snap.Version, Format, Version)
	}
	sortSnapshot(&snap)
	return &snap, nil
}

// normalize replaces nil argument maps with empty maps so identical
// states serialize identically.
func normalize(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	return args
}

func sortSnapshot(snap *Snapshot) {
	sort.Slice(snap.Queues, func(i, j int) bool { return snap.Queues[i].Name < snap.Queues[j].Name })
	sort.Slice(snap.Exchanges, func(i, j int) bool { return snap.Exchanges[i].Name < snap.Exchanges[j].Name })
	sort.Slice(snap.Bindings, func(i, j int) bool {
		return bindingKey(snap.Bindings[i]) < bindingKey(snap.Bindings[j])
	})
	sort.Slice(snap.Policies, func(i, j int) bool { return snap.Policies[i].Name < snap.Policies[j].Name })
}

func bindingKey(b BindingSpec) string {
	return b.Source + "\x00" + b.Destination + "\x00" + b.DestinationType + "\x00" + b.RoutingKey
}
