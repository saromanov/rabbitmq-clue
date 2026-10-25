package amqpop

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publishing carries the full AMQP 0-9-1 message properties we support
// in the publish form and when forwarding messages during replay.
type Publishing struct {
	Exchange      string         `json:"exchange"`
	RoutingKey    string         `json:"routing_key"`
	Body          []byte         `json:"-"`
	ContentType   string         `json:"content_type"`
	ContentEnc    string         `json:"content_encoding"`
	Headers       map[string]any `json:"headers"`
	DeliveryMode  uint8          `json:"delivery_mode"`
	Priority      uint8          `json:"priority"`
	Expiration    string         `json:"expiration"`
	CorrelationID string         `json:"correlation_id"`
	ReplyTo       string         `json:"reply_to"`
	MessageID     string         `json:"message_id"`
	Type          string         `json:"type"`
	AppID         string         `json:"app_id"`
	Timestamp     time.Time      `json:"timestamp"`
}

// Session bundles the live AMQP primitives needed for message operations.
type Session struct {
	Conn    *amqp.Connection
	Channel *amqp.Channel

	confirmM   sync.Mutex
	confirmSet bool
}

// Dial opens a connection and a channel.
func Dial(amqpURL string) (*Session, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("amqp channel: %w", err)
	}
	return &Session{Conn: conn, Channel: ch}, nil
}

// Close releases the channel and connection.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.Channel != nil {
		_ = s.Channel.Close()
	}
	if s.Conn != nil {
		_ = s.Conn.Close()
	}
}

// ensureConfirmMode switches the channel into publisher-confirm mode once.
func (s *Session) ensureConfirmMode() error {
	s.confirmM.Lock()
	defer s.confirmM.Unlock()
	if s.confirmSet {
		return nil
	}
	if err := s.Channel.Confirm(false); err != nil {
		return fmt.Errorf("confirm mode: %w", err)
	}
	s.confirmSet = true
	return nil
}

// Publish sends one message and, when confirm is set, waits for the
// broker confirm before returning. Called during replay for every
// message, so a failure here means the source message is not acked.
func (s *Session) Publish(ctx context.Context, p Publishing, confirm bool) error {
	msg := amqp.Publishing{
		Body:            p.Body,
		ContentType:     p.ContentType,
		ContentEncoding: p.ContentEnc,
		Headers:         p.Headers,
		DeliveryMode:    p.DeliveryMode,
		Priority:        p.Priority,
		Expiration:      p.Expiration,
		CorrelationId:   p.CorrelationID,
		ReplyTo:         p.ReplyTo,
		MessageId:       p.MessageID,
		Type:            p.Type,
		AppId:           p.AppID,
		Timestamp:       p.Timestamp,
	}
	if !confirm {
		if err := s.Channel.PublishWithContext(ctx, p.Exchange, p.RoutingKey, false, false, msg); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		return nil
	}
	if err := s.ensureConfirmMode(); err != nil {
		return err
	}
	dc, err := s.Channel.PublishWithDeferredConfirmWithContext(ctx, p.Exchange, p.RoutingKey, false, false, msg)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	confirmCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	acked, err := dc.WaitContext(confirmCtx)
	if err != nil {
		return fmt.Errorf("publish confirm: %w", err)
	}
	if !acked {
		return fmt.Errorf("publish nacked by broker")
	}
	return nil
}

// GetOne fetches a single message from a queue without requeueing.
// Returns ErrQueueEmpty when the queue is drained.
func (s *Session) GetOne(queue string) (amqp.Delivery, bool, error) {
	d, ok, err := s.Channel.Get(queue, false)
	if err != nil {
		return amqp.Delivery{}, false, fmt.Errorf("basic.get %q: %w", queue, err)
	}
	return d, ok, nil
}

// GetMany fetches up to limit messages requeueing none, returning them
// in queue order. Stops on the first empty response.
func (s *Session) GetMany(queue string, limit int) ([]amqp.Delivery, error) {
	if limit <= 0 {
		limit = 1000
	}
	var out []amqp.Delivery
	for len(out) < limit {
		d, ok, err := s.GetOne(queue)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, d)
	}
	return out, nil
}
