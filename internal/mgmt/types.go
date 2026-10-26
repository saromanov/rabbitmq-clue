package mgmt

// RateDetails carries the rolling rate information the management API
// attaches to counters ("publish_details": {"rate": 3.5, ...}).
type RateDetails struct {
	Rate     float64 `json:"rate"`
	LastEmit int64   `json:"last_emit"`
}

// MessageStats holds per-queue message rates as reported by the management API.
type MessageStats struct {
	Publish        int          `json:"publish"`
	PublishDetails *RateDetails `json:"publish_details,omitempty"`
	DeliverGet     int          `json:"deliver_get"`
	DeliverDetails *RateDetails `json:"deliver_details,omitempty"`
	Ack            int          `json:"ack"`
	AckDetails     *RateDetails `json:"ack_details,omitempty"`
	Redeliver      int          `json:"redeliver"`
}

// Queue is a subset of the management API queue object.
type Queue struct {
	Name                      string         `json:"name"`
	VHost                     string         `json:"vhost"`
	Type                      string         `json:"type"`
	Durable                   bool           `json:"durable"`
	AutoDelete                bool           `json:"auto_delete"`
	Exclusive                 bool           `json:"exclusive"`
	Arguments                 map[string]any `json:"arguments"`
	Messages                  int            `json:"messages"`
	MessagesReady             int            `json:"messages_ready"`
	MessagesUnacknowledged    int            `json:"messages_unacknowledged"`
	Consumers                 int            `json:"consumers"`
	MessageStats              *MessageStats  `json:"message_stats"`
	PublishInStats            *MessageStats  `json:"publish_in"`
	PublishOutStats           *MessageStats  `json:"publish_out"`
	State                     string         `json:"state"`
	EffectivePolicyDefinition map[string]any `json:"effective_policy_definition,omitempty"`
}

// Exchange is a subset of the management API exchange object.
type Exchange struct {
	Name       string         `json:"name"`
	VHost      string         `json:"vhost"`
	Type       string         `json:"type"`
	Durable    bool           `json:"durable"`
	AutoDelete bool           `json:"auto_delete"`
	Internal   bool           `json:"internal"`
	Arguments  map[string]any `json:"arguments"`
}

// Binding is a subset of the management API binding object.
type Binding struct {
	Source          string         `json:"source"`
	Destination     string         `json:"destination"`
	DestinationType string         `json:"destination_type"`
	VHost           string         `json:"vhost"`
	RoutingKey      string         `json:"routing_key"`
	Arguments       map[string]any `json:"arguments"`
}

// Policy is a subset of the management API policy object.
type Policy struct {
	Name       string         `json:"name"`
	VHost      string         `json:"vhost"`
	ApplyTo    string         `json:"apply-to"`
	Pattern    string         `json:"pattern"`
	Priority   int            `json:"priority"`
	Definition map[string]any `json:"definition"`
}

// VHost is a subset of the management API virtual host object.
type VHost struct {
	Name     string `json:"name"`
	Tracing  bool   `json:"tracing"`
	Clusters []any  `json:"clusters,omitempty"`
}

// Overview is a subset of the management API overview object.
type Overview struct {
	ManagementVersion string        `json:"management_version"`
	RabbitMQVersion   string        `json:"rabbitmq_version"`
	Node              string        `json:"node"`
	ClusterName       string        `json:"cluster_name"`
	Listeners         []any         `json:"listeners"`
	Queues            int           `json:"queues"`
	ExchangeTypes     []any         `json:"exchange_types"`
	MessageStats      *MessageStats `json:"message_stats"`
	QueueTotals       *QueueTotals  `json:"queue_totals"`
	ObjectTotals      *ObjectTotals `json:"object_totals"`
}

// QueueTotals aggregates queue message counts.
type QueueTotals struct {
	Messages               int64 `json:"messages"`
	MessagesReady          int64 `json:"messages_ready"`
	MessagesUnacknowledged int64 `json:"messages_unacknowledged"`
}

// ObjectTotals aggregates broker object counts.
type ObjectTotals struct {
	Consumers   int64 `json:"consumers"`
	Queues      int64 `json:"queues"`
	Exchanges   int64 `json:"exchanges"`
	Connections int64 `json:"connections"`
	Channels    int64 `json:"channels"`
}

// Message is a single message returned by the GET-messages endpoint
// (POST /api/queues/{vhost}/{name}/get).
type Message struct {
	PayloadBytes    int            `json:"payload_bytes"`
	Payload         string         `json:"payload"`
	PayloadEncoding string         `json:"payload_encoding"`
	Redelivered     bool           `json:"redelivered"`
	Exchange        string         `json:"exchange"`
	RoutingKey      string         `json:"routing_key"`
	MessageCount    int            `json:"message_count"`
	Properties      map[string]any `json:"properties"`
}

// PublishMessage is the request body for peek/publish through the
// management API. Only used for peek here; real publishing goes via AMQP.
type PublishMessage struct {
	Count    int    `json:"count"`
	Ackmode  string `json:"ackmode"`
	Truncate int    `json:"truncate"`
	Encoding string `json:"encoding"`
}

const (
	// AckRequeueTrue returns messages to their original position with
	// the documented caveat that they may be re-ordered.
	AckRequeueTrue = "ack_requeue_true"
)
