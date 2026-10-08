package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServeSSERemoteRequiresTLS: remote mode without a certificate and key
// must refuse to start instead of serving the bearer token in plaintext
// (Aikido 806869827).
func TestServeSSERemoteRequiresTLS(t *testing.T) {
	cases := []struct {
		name      string
		cert, key string
	}{
		{"no cert or key", "", ""},
		{"cert only", "server.crt", ""},
		{"key only", "", "server.key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Transport = "sse"
			cfg.Port = 0
			cfg.Remote = true
			cfg.CertFile = tc.cert
			cfg.KeyFile = tc.key
			cfg.TokenFile = filepath.Join(t.TempDir(), "server.token")
			s := New(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			start := time.Now()
			err := s.Serve(ctx)
			if err == nil {
				t.Fatal("remote SSE without a TLS certificate and key started; want an error")
			}
			if !strings.Contains(err.Error(), "TLS") {
				t.Fatalf("error = %v, want one naming TLS", err)
			}
			if time.Since(start) > time.Second {
				t.Fatalf("refusal took %s; want it before binding", time.Since(start))
			}
		})
	}
}

// TestServeSSEPartialTLSPairRejected: a cert without a key (or the reverse)
// is a configuration error even on loopback, not a silent plaintext start.
func TestServeSSEPartialTLSPairRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Transport = "sse"
	cfg.Port = 0
	cfg.CertFile = "server.crt"
	cfg.TokenFile = filepath.Join(t.TempDir(), "server.token")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := New(cfg).Serve(ctx)
	if err == nil || !strings.Contains(err.Error(), "--cert") {
		t.Fatalf("Serve = %v, want an error naming the missing --key/--cert pair", err)
	}
}
