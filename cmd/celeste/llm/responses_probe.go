package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// CompactionSupport is what a live probe learned about an endpoint's
// server-side compaction (/responses/compact, 2.0 W8).
type CompactionSupport int

const (
	// CompactionUnknown: the probe got no definite answer (auth, rate
	// limit, server error, network). Use the client ladder this time.
	CompactionUnknown CompactionSupport = iota
	// CompactionSupported: the endpoint serves /responses/compact.
	CompactionSupported
	// CompactionUnsupported: it does not; use the client ladder.
	CompactionUnsupported
)

func (s CompactionSupport) String() string {
	switch s {
	case CompactionSupported:
		return "supported"
	case CompactionUnsupported:
		return "unsupported"
	}
	return "unknown"
}

// CompactionProber is implemented by backends that can ask their endpoint
// whether it serves server-side compaction. W1 decides from it whether a
// session compacts on the server or on the client ladder.
type CompactionProber interface {
	ProbeCompaction(ctx context.Context) CompactionSupport
}

// compactProbes caches definite probe answers per endpoint for the life of
// the process (ruling 13).
var compactProbes sync.Map

func resetCompactProbes() {
	compactProbes.Range(func(k, _ any) bool {
		compactProbes.Delete(k)
		return true
	})
}

// compactProbeTimeout bounds one probe.
const compactProbeTimeout = 10 * time.Second

// ProbeCompaction asks the endpoint once per process whether it serves
// POST /responses/compact, with an empty input that any endpoint serving
// the route rejects without running a model. 2xx, 400 or 422 mean the route
// exists; an unsupported-endpoint answer (ruling 8) means it does not. Only
// those answers are cached; anything else is CompactionUnknown and is asked
// again next time. An endpoint in fallback is unsupported without a request.
func (b *ResponsesBackend) ProbeCompaction(ctx context.Context) CompactionSupport {
	if responsesFellBack(b.baseURL) {
		return CompactionUnsupported
	}
	key := endpointKey(b.baseURL)
	if v, ok := compactProbes.Load(key); ok {
		return v.(CompactionSupport)
	}
	ctx, cancel := context.WithTimeout(ctx, compactProbeTimeout)
	defer cancel()
	_, err := b.client.CompactResponse(ctx, openai.CompactResponseRequest{Model: b.config.Model, Input: []json.RawMessage{}})
	got := classifyCompactProbe(err)
	if got != CompactionUnknown {
		compactProbes.Store(key, got)
	}
	tui.LogInfo(fmt.Sprintf("openai: server compaction on %s: %s", key, got))
	return got
}

func classifyCompactProbe(err error) CompactionSupport {
	if err == nil {
		return CompactionSupported
	}
	if isUnsupportedEndpoint(err) {
		return CompactionUnsupported
	}
	status, _, _, ok := apiStatus(err)
	if ok && (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity) {
		return CompactionSupported
	}
	return CompactionUnknown
}
