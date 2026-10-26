package topology

import (
	"encoding/json"
	"fmt"
)

// ChangeKind classifies an object-level difference.
type ChangeKind string

const (
	// Added means the object exists only in the target snapshot.
	Added ChangeKind = "added"
	// Removed means the object exists only in the source snapshot.
	Removed ChangeKind = "removed"
	// Changed means the object exists in both snapshots but differs.
	Changed ChangeKind = "changed"
)

// FieldDiff describes a single attribute difference of a changed object.
// A nil From or To marks the attribute as absent on that side.
type FieldDiff struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// QueueChange is one queue difference.
type QueueChange struct {
	Kind   ChangeKind  `json:"kind"`
	Name   string      `json:"name"`
	From   *QueueSpec  `json:"from,omitempty"`
	To     *QueueSpec  `json:"to,omitempty"`
	Fields []FieldDiff `json:"fields,omitempty"`
}

// ExchangeChange is one exchange difference.
type ExchangeChange struct {
	Kind   ChangeKind    `json:"kind"`
	Name   string        `json:"name"`
	From   *ExchangeSpec `json:"from,omitempty"`
	To     *ExchangeSpec `json:"to,omitempty"`
	Fields []FieldDiff   `json:"fields,omitempty"`
}

// BindingChange is one binding difference. Bindings are identified by
// (source, destination, destination type, routing key); argument
// differences make a binding changed, not added and removed. The changed
// binding's identity is read from From or To.
type BindingChange struct {
	Kind   ChangeKind   `json:"kind"`
	From   *BindingSpec `json:"from,omitempty"`
	To     *BindingSpec `json:"to,omitempty"`
	Fields []FieldDiff  `json:"fields,omitempty"`
}

// PolicyChange is one policy difference.
type PolicyChange struct {
	Kind   ChangeKind  `json:"kind"`
	Name   string      `json:"name"`
	From   *PolicySpec `json:"from,omitempty"`
	To     *PolicySpec `json:"to,omitempty"`
	Fields []FieldDiff `json:"fields,omitempty"`
}

// Summary counts the differences of a report.
type Summary struct {
	Identical bool `json:"identical"`
	Added     int  `json:"added"`
	Removed   int  `json:"removed"`
	Changed   int  `json:"changed"`
}

// Report is the full result of comparing two snapshots. Objects are
// listed by name within each section; sections without differences are
// left nil so JSON output stays compact.
type Report struct {
	From      string           `json:"from"`
	To        string           `json:"to"`
	VHost     string           `json:"vhost"`
	Queues    []QueueChange    `json:"queues,omitempty"`
	Exchanges []ExchangeChange `json:"exchanges,omitempty"`
	Bindings  []BindingChange  `json:"bindings,omitempty"`
	Policies  []PolicyChange   `json:"policies,omitempty"`
	Summary   Summary          `json:"summary"`
}

// Diff compares two snapshots and produces a structured report.
// Both snapshots must be sorted (Capture and Load guarantee this).
func Diff(from, to *Snapshot) *Report {
	report := &Report{From: from.Source, To: to.Source}
	report.VHost = from.VHost
	if from.VHost != to.VHost {
		report.VHost = fmt.Sprintf("%s → %s", from.VHost, to.VHost)
	}

	i, j := 0, 0
	for i < len(from.Queues) || j < len(to.Queues) {
		switch {
		case j >= len(to.Queues) || (i < len(from.Queues) && from.Queues[i].Name < to.Queues[j].Name):
			report.Queues = append(report.Queues, QueueChange{Kind: Removed, Name: from.Queues[i].Name, From: &from.Queues[i]})
			i++
		case i >= len(from.Queues) || to.Queues[j].Name < from.Queues[i].Name:
			report.Queues = append(report.Queues, QueueChange{Kind: Added, Name: to.Queues[j].Name, To: &to.Queues[j]})
			j++
		default:
			if fields := diffQueueFields(from.Queues[i], to.Queues[j]); fields != nil {
				report.Queues = append(report.Queues, QueueChange{
					Kind: Changed, Name: from.Queues[i].Name,
					From: &from.Queues[i], To: &to.Queues[j], Fields: fields,
				})
			}
			i++
			j++
		}
	}

	i, j = 0, 0
	for i < len(from.Exchanges) || j < len(to.Exchanges) {
		switch {
		case j >= len(to.Exchanges) || (i < len(from.Exchanges) && from.Exchanges[i].Name < to.Exchanges[j].Name):
			report.Exchanges = append(report.Exchanges, ExchangeChange{Kind: Removed, Name: from.Exchanges[i].Name, From: &from.Exchanges[i]})
			i++
		case i >= len(from.Exchanges) || to.Exchanges[j].Name < from.Exchanges[i].Name:
			report.Exchanges = append(report.Exchanges, ExchangeChange{Kind: Added, Name: to.Exchanges[j].Name, To: &to.Exchanges[j]})
			j++
		default:
			if fields := diffExchangeFields(from.Exchanges[i], to.Exchanges[j]); fields != nil {
				report.Exchanges = append(report.Exchanges, ExchangeChange{
					Kind: Changed, Name: from.Exchanges[i].Name,
					From: &from.Exchanges[i], To: &to.Exchanges[j], Fields: fields,
				})
			}
			i++
			j++
		}
	}

	i, j = 0, 0
	for i < len(from.Bindings) || j < len(to.Bindings) {
		fk, tk := "", ""
		if i < len(from.Bindings) {
			fk = bindingKey(from.Bindings[i])
		}
		if j < len(to.Bindings) {
			tk = bindingKey(to.Bindings[j])
		}
		switch {
		case j >= len(to.Bindings) || (i < len(from.Bindings) && fk < tk):
			report.Bindings = append(report.Bindings, BindingChange{Kind: Removed, From: &from.Bindings[i]})
			i++
		case i >= len(from.Bindings) || tk < fk:
			report.Bindings = append(report.Bindings, BindingChange{Kind: Added, To: &to.Bindings[j]})
			j++
		default:
			if fields := diffBindingFields(from.Bindings[i], to.Bindings[j]); fields != nil {
				report.Bindings = append(report.Bindings, BindingChange{
					Kind: Changed,
					From: &from.Bindings[i], To: &to.Bindings[j], Fields: fields,
				})
			}
			i++
			j++
		}
	}

	i, j = 0, 0
	for i < len(from.Policies) || j < len(to.Policies) {
		switch {
		case j >= len(to.Policies) || (i < len(from.Policies) && from.Policies[i].Name < to.Policies[j].Name):
			report.Policies = append(report.Policies, PolicyChange{Kind: Removed, Name: from.Policies[i].Name, From: &from.Policies[i]})
			i++
		case i >= len(from.Policies) || to.Policies[j].Name < from.Policies[i].Name:
			report.Policies = append(report.Policies, PolicyChange{Kind: Added, Name: to.Policies[j].Name, To: &to.Policies[j]})
			j++
		default:
			if fields := diffPolicyFields(from.Policies[i], to.Policies[j]); fields != nil {
				report.Policies = append(report.Policies, PolicyChange{
					Kind: Changed, Name: from.Policies[i].Name,
					From: &from.Policies[i], To: &to.Policies[j], Fields: fields,
				})
			}
			i++
			j++
		}
	}

	for _, c := range report.Queues {
		count(&report.Summary, c.Kind)
	}
	for _, c := range report.Exchanges {
		count(&report.Summary, c.Kind)
	}
	for _, c := range report.Bindings {
		count(&report.Summary, c.Kind)
	}
	for _, c := range report.Policies {
		count(&report.Summary, c.Kind)
	}
	report.Summary.Identical = report.Summary.Added+report.Summary.Removed+report.Summary.Changed == 0
	return report
}

func count(s *Summary, kind ChangeKind) {
	switch kind {
	case Added:
		s.Added++
	case Removed:
		s.Removed++
	case Changed:
		s.Changed++
	}
}

func diffQueueFields(from, to QueueSpec) []FieldDiff {
	fields := scalarDiffs([]scalarPair{
		{"type", from.Type, to.Type},
		{"durable", from.Durable, to.Durable},
		{"auto_delete", from.AutoDelete, to.AutoDelete},
		{"exclusive", from.Exclusive, to.Exclusive},
	})
	return append(fields, argsDiff("arguments", from.Arguments, to.Arguments)...)
}

func diffExchangeFields(from, to ExchangeSpec) []FieldDiff {
	fields := scalarDiffs([]scalarPair{
		{"type", from.Type, to.Type},
		{"durable", from.Durable, to.Durable},
		{"auto_delete", from.AutoDelete, to.AutoDelete},
		{"internal", from.Internal, to.Internal},
	})
	return append(fields, argsDiff("arguments", from.Arguments, to.Arguments)...)
}

func diffBindingFields(from, to BindingSpec) []FieldDiff {
	return argsDiff("arguments", from.Arguments, to.Arguments)
}

func diffPolicyFields(from, to PolicySpec) []FieldDiff {
	fields := scalarDiffs([]scalarPair{
		{"apply_to", from.ApplyTo, to.ApplyTo},
		{"pattern", from.Pattern, to.Pattern},
		{"priority", from.Priority, to.Priority},
	})
	return append(fields, argsDiff("definition", from.Definition, to.Definition)...)
}

type scalarPair struct {
	field string
	from  any
	to    any
}

func scalarDiffs(pairs []scalarPair) []FieldDiff {
	out := []FieldDiff{}
	for _, p := range pairs {
		if fmt.Sprint(p.from) != fmt.Sprint(p.to) {
			out = append(out, FieldDiff{Field: p.field, From: p.from, To: p.to})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// argsDiff compares argument maps key by key. Missing keys are reported
// with a nil value on the absent side; values are compared by their
// canonical JSON, so numbers and nested maps compare structurally.
func argsDiff(field string, from, to map[string]any) []FieldDiff {
	out := []FieldDiff{}
	keys := map[string]struct{}{}
	for k := range from {
		keys[k] = struct{}{}
	}
	for k := range to {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sortStrings(sorted)
	for _, k := range sorted {
		fv, fok := from[k]
		tv, tok := to[k]
		switch {
		case fok && !tok:
			out = append(out, FieldDiff{Field: field + "." + k, From: fv, To: nil})
		case !fok && tok:
			out = append(out, FieldDiff{Field: field + "." + k, From: nil, To: tv})
		case canonicalJSON(fv) != canonicalJSON(tv):
			out = append(out, FieldDiff{Field: field + "." + k, From: fv, To: tv})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// canonicalJSON renders a value with sorted map keys so argument values
// compare structurally regardless of JSON object ordering.
// encoding/json marshals map keys in sorted order, which is exactly that.
func canonicalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
