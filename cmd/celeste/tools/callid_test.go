package tools

import (
	"context"
	"testing"
)

func TestCallIDTravelsOnTheContext(t *testing.T) {
	if got := CallIDFromContext(context.Background()); got != "" {
		t.Fatalf("no call ID: got %q", got)
	}
	if got := CallIDFromContext(WithCallID(context.Background(), "call_7")); got != "call_7" {
		t.Fatalf("got %q, want call_7", got)
	}
}
