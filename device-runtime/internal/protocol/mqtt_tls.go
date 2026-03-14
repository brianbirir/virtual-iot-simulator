package protocol

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// newTLSConfig builds a *tls.Config for MQTT TLS connections.
// caCertPath is required; clientCert/clientKey are optional (mutual TLS).
func newTLSConfig(caCertPath, clientCertPath, clientKeyPath string) (*tls.Config, error) {
	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert %q: %w", caCertPath, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid CA cert found in %q", caCertPath)
	}

	cfg := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}

	if clientCertPath != "" && clientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}
