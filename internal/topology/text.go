package topology

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText formats a report as a readable diff listing suitable for
// terminals and curl output.
func RenderText(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "topology diff: %s → %s [vhost %s]\n", r.From, r.To, r.VHost)

	section := func(title string, count int) {
		if count > 0 {
			fmt.Fprintf(&b, "\n%s (%d):\n", title, count)
		}
	}
	section("queues", len(r.Queues))
	for _, c := range r.Queues {
		if c.Kind == Changed {
			fmt.Fprintf(&b, "  %s %s\n", kindMarker(c.Kind), c.Name)
			writeFields(&b, c.Fields)
			continue
		}
		spec := c.From
		if c.Kind == Added {
			spec = c.To
		}
		fmt.Fprintf(&b, "  %s %s (%s)\n", kindMarker(c.Kind), c.Name, describeSpec(spec))
	}

	section("exchanges", len(r.Exchanges))
	for _, c := range r.Exchanges {
		if c.Kind == Changed {
			fmt.Fprintf(&b, "  %s %s\n", kindMarker(c.Kind), c.Name)
			writeFields(&b, c.Fields)
			continue
		}
		spec := c.From
		if c.Kind == Added {
			spec = c.To
		}
		fmt.Fprintf(&b, "  %s %s (%s)\n", kindMarker(c.Kind), c.Name, describeExchange(spec))
	}

	section("bindings", len(r.Bindings))
	for _, c := range r.Bindings {
		spec := c.From
		if spec == nil {
			spec = c.To
		}
		fmt.Fprintf(&b, "  %s %s -> %s [%s]\n", kindMarker(c.Kind), spec.Source, spec.Destination, spec.RoutingKey)
		writeFields(&b, c.Fields)
	}

	section("policies", len(r.Policies))
	for _, c := range r.Policies {
		if c.Kind == Changed {
			fmt.Fprintf(&b, "  %s %s\n", kindMarker(c.Kind), c.Name)
			writeFields(&b, c.Fields)
			continue
		}
		spec := c.From
		if c.Kind == Added {
			spec = c.To
		}
		fmt.Fprintf(&b, "  %s %s (%s, %q)\n", kindMarker(c.Kind), c.Name, spec.ApplyTo, spec.Pattern)
		writeFields(&b, c.Fields)
	}

	if r.Summary.Identical {
		b.WriteString("\nidentical\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\nsummary: %d difference(s) — %d added, %d removed, %d changed\n",
		r.Summary.Added+r.Summary.Removed+r.Summary.Changed, r.Summary.Added, r.Summary.Removed, r.Summary.Changed)
	return b.String()
}

func kindMarker(kind ChangeKind) string {
	switch kind {
	case Added:
		return "+"
	case Removed:
		return "-"
	default:
		return "~"
	}
}

func describeSpec(q *QueueSpec) string {
	parts := []string{"type=" + orClassic(q.Type)}
	if q.Durable {
		parts = append(parts, "durable")
	}
	if q.AutoDelete {
		parts = append(parts, "auto_delete")
	}
	if q.Exclusive {
		parts = append(parts, "exclusive")
	}
	return strings.Join(parts, ", ")
}

func describeExchange(e *ExchangeSpec) string {
	parts := []string{"type=" + orClassic(e.Type)}
	if e.Durable {
		parts = append(parts, "durable")
	}
	if e.AutoDelete {
		parts = append(parts, "auto_delete")
	}
	if e.Internal {
		parts = append(parts, "internal")
	}
	return strings.Join(parts, ", ")
}

func orClassic(t string) string {
	if t == "" {
		return "classic"
	}
	return t
}

func writeFields(b *strings.Builder, fields []FieldDiff) {
	for _, f := range fields {
		fmt.Fprintf(b, "      %s: %s → %s\n", f.Field, renderValue(f.From), renderValue(f.To))
	}
}

func renderValue(v any) string {
	if v == nil {
		return "(absent)"
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
