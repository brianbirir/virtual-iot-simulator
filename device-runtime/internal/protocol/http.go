package protocol

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPConfig holds connection parameters for the HTTP publisher.
type HTTPConfig struct {
	Endpoint    string        // POST target URL, e.g. "http://localhost:8888/telemetry"
	Timeout     time.Duration // per-request timeout; default 10s
	MaxIdleConn int           // http.Transport MaxIdleConnsPerHost; default 20
}

// DefaultHTTPConfig returns sensible defaults for the given endpoint URL.
func DefaultHTTPConfig(endpoint string) HTTPConfig {
	return HTTPConfig{
		Endpoint:    endpoint,
		Timeout:     10 * time.Second,
		MaxIdleConn: 20,
	}
}

// HTTPPublisher POSTs JSON telemetry to a configurable REST endpoint.
// It is safe for concurrent use.
type HTTPPublisher struct {
	endpoint string
	client   *http.Client
}

// NewHTTPPublisher creates an HTTPPublisher with a shared http.Client.
func NewHTTPPublisher(cfg HTTPConfig) *HTTPPublisher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = cfg.MaxIdleConn

	return &HTTPPublisher{
		endpoint: cfg.Endpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}
}

// Publish POSTs payload as application/json to the configured endpoint.
// The topic is appended as a query parameter: ?topic=<topic>.
func (p *HTTPPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	url := fmt.Sprintf("%s?topic=%s", p.endpoint, topic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP publish: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP publish: server returned %d", resp.StatusCode)
	}
	return nil
}

// Close is a no-op; the shared http.Client manages its own connection pool.
func (p *HTTPPublisher) Close() error { return nil }
