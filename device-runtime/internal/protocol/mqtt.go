package protocol

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// MQTTConfig holds connection parameters for the MQTT publisher.
type MQTTConfig struct {
	BrokerURL      string        // e.g. "tcp://localhost:1883" or "ssl://broker:8883"
	QoS            byte          // 0, 1, or 2
	ConnectTimeout time.Duration // default 10s
	KeepAlive      time.Duration // default 30s
	CleanSession   bool          // default true
	CACertPath     string        // optional, for TLS
	ClientCertPath string        // optional, for mutual TLS
	ClientKeyPath  string        // optional, for mutual TLS
	PoolSize       int           // number of MQTT connections to maintain; 0/1 = single connection
}

// DefaultMQTTConfig returns sensible defaults.
func DefaultMQTTConfig(brokerURL string) MQTTConfig {
	return MQTTConfig{
		BrokerURL:      brokerURL,
		QoS:            1,
		ConnectTimeout: 10 * time.Second,
		KeepAlive:      30 * time.Second,
		CleanSession:   true,
	}
}

// MQTTPublisher publishes telemetry to an MQTT broker.
// It is safe for concurrent use.
type MQTTPublisher struct {
	client   mqtt.Client
	qos      byte
	clientID string
}

var mqttClientSeq atomic.Int64

// NewMQTTPublisher creates and connects an MQTT publisher.
func NewMQTTPublisher(cfg MQTTConfig) (*MQTTPublisher, error) {
	id := fmt.Sprintf("iot-sim-runtime-%d", mqttClientSeq.Add(1))

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(id).
		SetCleanSession(cfg.CleanSession).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetKeepAlive(cfg.KeepAlive).
		SetOnConnectHandler(func(_ mqtt.Client) {
			log.Info().Str("broker", cfg.BrokerURL).Str("client_id", id).Msg("MQTT connected")
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Warn().Str("broker", cfg.BrokerURL).Err(err).Msg("MQTT connection lost, reconnecting")
		})

	if cfg.CACertPath != "" {
		tlsCfg, err := newTLSConfig(cfg.CACertPath, cfg.ClientCertPath, cfg.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("MQTT TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsCfg)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(cfg.ConnectTimeout) {
		return nil, fmt.Errorf("MQTT connect timeout after %s", cfg.ConnectTimeout)
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("MQTT connect: %w", err)
	}

	return &MQTTPublisher{client: client, qos: cfg.QoS, clientID: id}, nil
}

// Publish sends payload to topic. It respects ctx cancellation during the wait.
func (p *MQTTPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, p.qos, false, payload)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
		return token.Error()
	}
}

// Close disconnects from the broker.
func (p *MQTTPublisher) Close() error {
	p.client.Disconnect(250)
	return nil
}

// MQTTPool manages a fixed pool of MQTT connections and distributes Publish
// calls across them in round-robin order.
type MQTTPool struct {
	conns  []*MQTTPublisher
	cursor atomic.Int64
}

// NewMQTTPool creates n MQTT connections using cfg. Returns an error if any
// connection fails.
func NewMQTTPool(cfg MQTTConfig) (*MQTTPool, error) {
	n := cfg.PoolSize
	if n < 1 {
		n = 1
	}
	pool := &MQTTPool{conns: make([]*MQTTPublisher, 0, n)}
	for i := 0; i < n; i++ {
		p, err := NewMQTTPublisher(cfg)
		if err != nil {
			// Close already-connected members before returning.
			for _, c := range pool.conns {
				c.Close() //nolint:errcheck
			}
			return nil, fmt.Errorf("MQTT pool connection %d: %w", i, err)
		}
		pool.conns = append(pool.conns, p)
	}
	return pool, nil
}

// Publish forwards to the next connection in the pool (round-robin).
func (p *MQTTPool) Publish(ctx context.Context, topic string, payload []byte) error {
	idx := p.cursor.Add(1) % int64(len(p.conns))
	return p.conns[idx].Publish(ctx, topic, payload)
}

// Close disconnects all pooled connections.
func (p *MQTTPool) Close() error {
	for _, c := range p.conns {
		c.Close() //nolint:errcheck
	}
	return nil
}
