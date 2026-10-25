package mgmt

import (
	"time"
)

// DeathStep is one entry of the x-death header chain: the record RabbitMQ
// appends every time a message is dead-lettered.
type DeathStep struct {
	Queue    string `json:"queue"`
	Reason   string `json:"reason"`
	Time     string `json:"time"`
	Exchange string `json:"exchange"`
	Count    int64  `json:"count"`
}

// HeaderMap is a permissive accessor over AMQP header maps, which may hold
// nested maps, strings and integers of varying concrete types.
type HeaderMap map[string]any

// DeathChain extracts and normalises the x-death chain from message
// headers, newest death first, mirroring RabbitMQ ordering guarantees.
func (h HeaderMap) DeathChain() []DeathStep {
	raw, ok := h["x-death"].([]any)
	if !ok {
		return nil
	}
	steps := make([]DeathStep, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		step := DeathStep{
			Queue:    asString(m["queue"]),
			Reason:   asString(m["reason"]),
			Time:     asString(m["time"]),
			Exchange: asString(m["exchange"]),
			Count:    asInt64(m["count"]),
		}
		steps = append(steps, step)
	}
	return steps
}

// TotalDeaths sums the per-queue death counts of the chain.
func (h HeaderMap) TotalDeaths() int64 {
	var total int64
	for _, step := range h.DeathChain() {
		total += step.Count
	}
	return total
}

// LastDeathTime returns the most recent dead-lettering time if parseable.
func (h HeaderMap) LastDeathTime() (time.Time, bool) {
	steps := h.DeathChain()
	for i := range steps {
		if t, ok := parseRMQTime(steps[i].Time); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// OriginQueue returns the queue the message was first dead-lettered from,
// which per RabbitMQ semantics is the last entry of the x-death chain.
func (h HeaderMap) OriginQueue() string {
	steps := h.DeathChain()
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Queue
}

// HeadTime returns the best available estimate of when the head message
// entered the broker: the message timestamp property if set, otherwise
// the last death time from x-death.
func (m Message) HeadTime() (time.Time, bool) {
	if ts, ok := asTime(m.Properties["timestamp"]); ok {
		return ts, true
	}
	if headers, ok := m.Properties["headers"].(map[string]any); ok {
		return HeaderMap(headers).LastDeathTime()
	}
	return time.Time{}, false
}

// parseRMQTime accepts the time formats RabbitMQ emits in x-death entries:
// RFC3339, "2006-01-02 15:04:05.000" and unix timestamps.
func parseRMQTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func asTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		return parseRMQTime(t)
	case map[string]any:
		// management API serialises timestamps as {"timestamp": seconds, "offset": ms}
		if v, ok := t["timestamp"]; ok {
			return time.Unix(asInt64(v), 0), true
		}
	}
	return time.Time{}, false
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}
