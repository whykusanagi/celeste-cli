package compact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
)

// A synchronous shadow report (the MCP chat's) asks Jev under the
// caller's context: a client that went away does not hold the handler
// for Jev's whole timeout.
func TestSyncShadowReportStopsWithTheCallersContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", `{"path":"f` + string(rune('a'+i)) + `.go"}`, 2_000})
	}
	msgs := history(steps...)
	ctx, cancel := context.WithCancel(context.Background())
	opts, report := WithJev(ctx, &jev.Client{Key: "k", URL: srv.URL}, "shadow", msgs, Options{Window: 20_000, Used: Estimate(msgs)}, func(string) {}, false)
	res := Plan(msgs, opts)
	if len(res.Edits) == 0 {
		t.Fatal("the plan elided nothing; the report would not ask Jev")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	report(res)
	if d := time.Since(start); d > time.Second {
		t.Errorf("report took %v after the caller's context was cancelled", d)
	}
}
