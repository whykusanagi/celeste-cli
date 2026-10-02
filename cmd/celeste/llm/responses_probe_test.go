package llm

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func compactRequests(srv *fakeprovider.Server) []fakeprovider.Request {
	var out []fakeprovider.Request
	for _, r := range srv.Requests() {
		if r.Path == "/v1/responses/compact" {
			out = append(out, r)
		}
	}
	return out
}

// Both paths the spec names: the route exists (supported) or it doesn't
// (the client ladder). Definite answers are asked once per endpoint;
// anything else is asked again.
func TestProbeCompaction(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   CompactionSupport
		cached bool
	}{
		{"compacts", 200, `{"id":"cmp_1","object":"response.compaction","created_at":0,"output":[]}`, CompactionSupported, true},
		{"route exists, input rejected", 400, `{"error":{"message":"input must not be empty","type":"invalid_request_error"}}`, CompactionSupported, true},
		{"no route", 404, `{"error":{"message":"Not found","type":"invalid_request_error"}}`, CompactionUnsupported, true},
		{"unsupported endpoint", 400, `{"error":{"message":"Unsupported endpoint","type":"invalid_request_error"}}`, CompactionUnsupported, true},
		{"overloaded", 503, `{"error":{"message":"overloaded","type":"server_error"}}`, CompactionUnknown, false},
		{"bad key", 401, `{"error":{"message":"Incorrect API key","type":"invalid_request_error"}}`, CompactionUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCompactProbes()
			t.Cleanup(resetCompactProbes)
			srv := fakeprovider.NewOpenAIResponses(t)
			srv.Handle("/v1/responses/compact", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			c, _ := newResponsesTestClient(t, srv, "gpt-test")
			for i := 0; i < 2; i++ {
				assert.Equal(t, tc.want, c.ServerCompaction(context.Background()), "call %d", i)
			}
			reqs := compactRequests(srv)
			if tc.cached {
				assert.Len(t, reqs, 1)
			} else {
				assert.Len(t, reqs, 2)
			}
			require.NotEmpty(t, reqs)
			assert.Equal(t, "gpt-test", reqs[0].Body["model"])
			assert.Equal(t, []any{}, reqs[0].Body["input"], "the probe must not run a model on real input")
		})
	}
}

func TestProbeCompactionSkipsEndpointsInFallback(t *testing.T) {
	resetCompactProbes()
	t.Cleanup(resetCompactProbes)
	srv := fakeprovider.NewOpenAIResponses(t)
	c, b := newResponsesTestClient(t, srv, "gpt-test")
	markResponsesFallback(b.baseURL, assert.AnError)
	assert.Equal(t, CompactionUnsupported, c.ServerCompaction(context.Background()))
	assert.Empty(t, srv.Requests())
}

func TestServerCompactionWithoutAProbe(t *testing.T) {
	c := NewClientWithBackend(&Config{}, nil, NewOpenAIBackend(&Config{}))
	assert.Equal(t, CompactionUnsupported, c.ServerCompaction(context.Background()))
	assert.Equal(t, "unsupported", CompactionUnsupported.String())
}

// The probe and fallback log lines name the endpoint without credentials a
// base URL may carry (userinfo, query).
func TestResponsesLogLinesRedactTheBaseURL(t *testing.T) {
	base := "https://user:s3cret@gw.example/v1?api_key=k3y#frag"
	for _, line := range []string{
		compactProbeLogLine(base, CompactionSupported),
		fallbackLogLine(base, assert.AnError),
	} {
		assert.NotContains(t, line, "s3cret")
		assert.NotContains(t, line, "k3y")
		assert.Contains(t, line, "gw.example/v1")
	}
}
