// Package journey reconstructs the dead-letter path of a message from its
// x-death headers: the hops it made between queues, the retries it burned
// on TTL+DLX ladders and where it is currently stuck.
package journey

import (
	"time"

	"github.com/saromanov/rabbitmq-clue/internal/mgmt"
)

// Hop is one stop of the message's dead-letter path, in chronological
// order. Queue is where the message died; Exchange and RoutingKeys are
// the exchange and routing keys the message was published to before it
// died in that queue.
type Hop struct {
	Queue       string   `json:"queue"`
	Reason      string   `json:"reason,omitempty"`
	Count       int64    `json:"count"`
	Time        string   `json:"time,omitempty"`
	Exchange    string   `json:"exchange,omitempty"`
	RoutingKeys []string `json:"routing_keys,omitempty"`
}

// Ladder is a retry ladder detected on one queue: the message died there
// more than once, the classic TTL+DLX exponential backoff topology
// (rabbitmq/rabbitmq-server#1449). Cycle marks the strict variant where
// every death had the same reason.
type Ladder struct {
	Queue    string   `json:"queue"`
	Attempts int64    `json:"attempts"`
	Reasons  []string `json:"reasons,omitempty"`
	Cycle    bool     `json:"cycle"`
}

// Journey is the reconstructed path of a dead-lettered message.
type Journey struct {
	OriginQueue    string   `json:"origin_queue,omitempty"`
	OriginExchange string   `json:"origin_exchange,omitempty"`
	OriginKeys     []string `json:"origin_routing_keys,omitempty"`
	CurrentQueue   string   `json:"current_queue,omitempty"`
	Hops           []Hop    `json:"hops,omitempty"`
	TotalDeaths    int64    `json:"total_deaths"`
	Retries        int64    `json:"retries"`
	Ladders        []Ladder `json:"ladders,omitempty"`
	LastDeathTime  string   `json:"last_death_time,omitempty"`
	AgeSec         float64  `json:"age_sec,omitempty"`
}

// Build reconstructs a journey from message headers. Returns nil when the
// message carries no x-death header (it never died). CurrentQueue is the
// queue the message sits in right now.
func Build(headers map[string]any, currentQueue string) *Journey {
	if headers == nil {
		return nil
	}
	h := mgmt.HeaderMap(headers)
	chain := h.DeathChain()
	if len(chain) == 0 {
		return nil
	}

	j := &Journey{CurrentQueue: currentQueue, TotalDeaths: h.TotalDeaths()}

	if first, ok := h.FirstDeath(); ok {
		j.OriginQueue = first.Queue
		j.OriginExchange = first.Exchange
		j.OriginKeys = first.RoutingKeys
	}

	// x-death is newest first; hops are presented oldest first.
	j.Hops = make([]Hop, 0, len(chain))
	deathsByQueue := map[string]int64{}
	reasonsByQueue := map[string][]string{}
	for i := len(chain) - 1; i >= 0; i-- {
		s := chain[i]
		j.Hops = append(j.Hops, Hop{
			Queue:       s.Queue,
			Reason:      s.Reason,
			Count:       s.Count,
			Time:        s.Time,
			Exchange:    s.Exchange,
			RoutingKeys: s.RoutingKeys,
		})
		deathsByQueue[s.Queue] += s.Count
		if s.Count > 1 {
			// x-death collapses repeated deaths of the same queue+reason
			// into one entry; unpack the retries for ladder detection.
			for n := int64(1); n < s.Count; n++ {
				reasonsByQueue[s.Queue] = appendUnique(reasonsByQueue[s.Queue], s.Reason)
			}
		}
		reasonsByQueue[s.Queue] = appendUnique(reasonsByQueue[s.Queue], s.Reason)
	}
	j.Retries = j.TotalDeaths - int64(len(chain))

	seenLadder := map[string]bool{}
	for _, hop := range j.Hops {
		deaths := deathsByQueue[hop.Queue]
		if deaths <= 1 || seenLadder[hop.Queue] {
			continue
		}
		seenLadder[hop.Queue] = true
		ladder := Ladder{Queue: hop.Queue, Attempts: deaths, Reasons: reasonsByQueue[hop.Queue]}
		ladder.Cycle = len(ladder.Reasons) <= 1
		j.Ladders = append(j.Ladders, ladder)
	}

	if last, ok := h.LastDeathTime(); ok {
		j.LastDeathTime = last.UTC().Format(time.RFC3339)
		j.AgeSec = time.Since(last).Seconds()
	}
	return j
}

func appendUnique(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}
