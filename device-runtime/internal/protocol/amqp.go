package protocol

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

// AMQPConfig holds connection parameters for the AMQP publisher.
type AMQPConfig struct {
	URL          string // e.g. "amqp://guest:guest@localhost:5672/"
	Exchange     string // exchange name
	ExchangeType string // "topic", "direct", "fanout"
}

// DefaultAMQPConfig returns sensible defaults.
func DefaultAMQPConfig(url string) AMQPConfig {
	return AMQPConfig{
		URL:          url,
		Exchange:     "iot.telemetry",
		ExchangeType: "topic",
	}
}

// AMQPPublisher publishes telemetry to a RabbitMQ exchange.
// One AMQP channel is allocated per goroutine via a sync.Pool to satisfy the
// AMQP spec requirement that channels are not shared between goroutines.
type AMQPPublisher struct {
	conn     *amqp.Connection
	cfg      AMQPConfig
	pool     sync.Pool
	mu       sync.Mutex // guards conn for reconnect
}

// NewAMQPPublisher dials an AMQP broker and declares the exchange.
func NewAMQPPublisher(cfg AMQPConfig) (*AMQPPublisher, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("AMQP dial: %w", err)
	}

	p := &AMQPPublisher{conn: conn, cfg: cfg}
	p.pool = sync.Pool{New: func() any {
		ch, err := p.conn.Channel()
		if err != nil {
			log.Warn().Err(err).Msg("AMQP channel create failed")
			return nil
		}
		// Declare exchange idempotently.
		if err := ch.ExchangeDeclare(cfg.Exchange, cfg.ExchangeType, true, false, false, false, nil); err != nil {
			log.Warn().Err(err).Str("exchange", cfg.Exchange).Msg("AMQP exchange declare failed")
			ch.Close() //nolint:errcheck
			return nil
		}
		return ch
	}}

	// Validate with a test channel.
	ch := p.pool.Get()
	if ch == nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("AMQP channel setup failed")
	}
	p.pool.Put(ch)

	log.Info().Str("url", cfg.URL).Str("exchange", cfg.Exchange).Msg("AMQP connected")
	return p, nil
}

// Publish routes payload to the configured exchange with topic as routing key.
func (p *AMQPPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	ch, ok := p.pool.Get().(*amqp.Channel)
	if !ok || ch == nil {
		return fmt.Errorf("AMQP: could not acquire channel")
	}
	defer p.pool.Put(ch)

	return ch.PublishWithContext(ctx,
		p.cfg.Exchange, // exchange
		topic,          // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         payload,
		},
	)
}

// Close disconnects from the broker.
func (p *AMQPPublisher) Close() error {
	return p.conn.Close()
}
