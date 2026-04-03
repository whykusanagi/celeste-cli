package server

import (
	"net/http"
	"testing"
	"time"
)

func TestTokenBucketAllow(t *testing.T) {
	tb := newTokenBucket(3) // 3 per minute

	// Should allow up to 3 requests immediately
	if !tb.allow() {
		t.Fatal("first request should be allowed")
	}
	if !tb.allow() {
		t.Fatal("second request should be allowed")
	}
	if !tb.allow() {
		t.Fatal("third request should be allowed")
	}
	// Fourth should be rejected
	if tb.allow() {
		t.Fatal("fourth request should be rejected")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := newTokenBucket(60) // 1 per second

	// Drain all tokens
	for i := 0; i < 60; i++ {
		tb.allow()
	}
	if tb.allow() {
		t.Fatal("should be empty")
	}

	// Simulate time passing
	tb.mu.Lock()
	tb.lastRefill = time.Now().Add(-2 * time.Second) // pretend 2 seconds passed
	tb.mu.Unlock()

	// Should have refilled ~2 tokens
	if !tb.allow() {
		t.Fatal("should have refilled after time passage")
	}
}

func TestValidateBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		auth     string
		expected string
		want     bool
	}{
		{"valid", "Bearer abc123", "abc123", true},
		{"wrong token", "Bearer wrong", "abc123", false},
		{"missing bearer", "abc123", "abc123", false},
		{"empty header", "", "abc123", false},
		{"too short", "Bear", "abc123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Header: http.Header{}}
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			got := validateBearerToken(r, tt.expected)
			if got != tt.want {
				t.Fatalf("validateBearerToken() = %v, want %v", got, tt.want)
			}
		})
	}
}
