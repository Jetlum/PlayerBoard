// Package bus is a thin wrapper over NATS JetStream for at-least-once event delivery.
package bus

import (
	"strings"

	"github.com/nats-io/nats.go"
)

// Subjects used across the system.
const (
	SubjectPerformance = "events.performance" // ingest -> worker
	SubjectMilestone   = "events.milestone"   // worker -> api (WS fan-out)
	StreamName         = "EVENTS"
)

type Bus struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

func Connect(url string) (*Bus, error) {
	nc, err := nats.Connect(url, nats.MaxReconnects(-1), nats.Name("playerboard"))
	if err != nil {
		return nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, err
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"events.>"},
	}); err != nil && !strings.Contains(err.Error(), "already in use") {
		nc.Close()
		return nil, err
	}
	return &Bus{nc: nc, js: js}, nil
}

func (b *Bus) Publish(subject string, data []byte) error {
	_, err := b.js.Publish(subject, data)
	return err
}

// Subscribe creates a durable push consumer that auto-acks when the handler returns nil.
// Convenient for best-effort fan-out (e.g. WebSocket push) where redelivery is cheap.
func (b *Bus) Subscribe(subject, durable string, handler func(subject string, data []byte) error) error {
	_, err := b.js.Subscribe(subject, func(m *nats.Msg) {
		if err := handler(m.Subject, m.Data); err != nil {
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	}, nats.Durable(durable), nats.ManualAck(), nats.AckExplicit())
	return err
}

// Delivery is a received message with explicit ack control, so a consumer can ack only
// after it has durably processed the message. Hides the nats.Msg from callers.
type Delivery struct {
	Subject string
	Data    []byte
	msg     *nats.Msg
}

func (d Delivery) Ack() { _ = d.msg.Ack() }
func (d Delivery) Nak() { _ = d.msg.Nak() }

// Consume creates a durable push consumer with manual ack and bounded in-flight messages.
// The handler owns ack/nak. MaxAckPending provides backpressure.
func (b *Bus) Consume(subject, durable string, handler func(Delivery)) error {
	_, err := b.js.Subscribe(subject, func(m *nats.Msg) {
		handler(Delivery{Subject: m.Subject, Data: m.Data, msg: m})
	}, nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.MaxAckPending(256))
	return err
}

func (b *Bus) Close() { b.nc.Drain() }
