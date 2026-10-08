package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// gatedTransport answers like mockTransport but blocks the second Receive
// (tools/list) until release is closed, so a test can act while a connect
// is in flight.
type gatedTransport struct {
	mu      sync.Mutex
	inner   *mockTransport
	n       int
	reached chan struct{}
	release chan struct{}
}

func (g *gatedTransport) Send(req *Request) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner.Send(req)
}

func (g *gatedTransport) SendNotification(n *Notification) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner.SendNotification(n)
}

func (g *gatedTransport) Receive() (*Response, error) {
	g.mu.Lock()
	g.n++
	n := g.n
	g.mu.Unlock()
	if n == 2 {
		close(g.reached)
		<-g.release
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner.Receive()
}

func (g *gatedTransport) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner.Close()
}

func (g *gatedTransport) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inner.closed
}

// TestConnectInFlightHonoursDisconnectAndStop: a Disconnect or Stop that
// lands while a connect is in flight wins: the connect installs no client
// and leaves no tools (Aikido 806869778).
func TestConnectInFlightHonoursDisconnectAndStop(t *testing.T) {
	for _, revoke := range []struct {
		name string
		do   func(m *Manager)
	}{
		{"disconnect", func(m *Manager) { _ = m.Disconnect("srv") }},
		{"stop", func(m *Manager) { _ = m.Stop() }},
	} {
		t.Run(revoke.name, func(t *testing.T) {
			registry := tools.NewRegistry()
			m := NewManager("", registry)
			gt := &gatedTransport{
				inner:   &mockTransport{responses: []*Response{makeInitResponse(), makeToolsListResponse("t1")}},
				reached: make(chan struct{}),
				release: make(chan struct{}),
			}
			done := make(chan error, 1)
			go func() {
				done <- m.connectClient(context.Background(), "srv", NewClient(gt, "celeste", "1.0"), "stdio", false)
			}()
			select {
			case <-gt.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("connect never reached tools/list")
			}
			revoke.do(m)
			close(gt.release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("connect did not return")
			}
			if m.IsConnected("srv") {
				t.Fatal("a connect revoked while in flight installed its client")
			}
			if n := registry.Count(); n != 0 {
				t.Fatalf("a revoked connect left %d tools registered", n)
			}
			if !gt.isClosed() {
				t.Fatal("a revoked connect left its transport open")
			}
		})
	}
}
