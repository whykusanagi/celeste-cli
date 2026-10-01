package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// strictProvider fronts a fake provider and rejects any model but served,
// the way a real provider rejects a retired one.
func strictProvider(t *testing.T, served string, inner *fakeprovider.Server) string {
	t.Helper()
	target, err := url.Parse(inner.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Model != served {
			http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusNotFound)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + target.Path
}

// An agent run configured on a retired model runs on the served one, and
// says so on stderr.
func TestAgentRunUsesResolvedModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()

	inner := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	base := strictProvider(t, "model-v2", inner)
	// The fake provider listens on 127.0.0.1 ("local"); give it a catalog.
	defer providers.SetCatalogForTest("local", []providers.CatalogModel{{ID: "model-v2", Default: true}})()

	cfg := &config.Config{APIKey: "k", BaseURL: base, Model: "model", Timeout: 10}
	var stderr strings.Builder
	resolveServedModels(cfg, &stderr)
	if !strings.Contains(stderr.String(), "no longer serves model; using model-v2") {
		t.Errorf("stderr = %q", stderr.String())
	}

	opts := agent.DefaultOptions()
	opts.Workspace = ws
	opts.EnablePlanning = false
	opts.RequireVerification = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	r, err := agent.NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	state, err := r.RunGoal(context.Background(), "say ok")
	if err != nil {
		t.Fatalf("run failed (the retired model was sent?): %v", err)
	}
	if state.Status != agent.StatusCompleted {
		t.Errorf("status = %s", state.Status)
	}
	if got := inner.Requests()[0].Body["model"]; got != "model-v2" {
		t.Errorf("model sent = %v", got)
	}
}
