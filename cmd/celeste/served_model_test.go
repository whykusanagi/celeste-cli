package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
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

// fakeOpenAIDotCom serves a provider at http://api.openai.com/v1 for this
// test only: every connection the default transport opens goes to a local
// server, so celeste detects the provider as OpenAI (catalog, per-model
// checks) and nothing leaves the machine. The server lists listed at
// /v1/models, answers /v1/models/{id} with 200 for listed and aliases and 404
// otherwise, and rejects chat requests for any other model.
func fakeOpenAIDotCom(t *testing.T, listed, aliases []string, inner *fakeprovider.Server) {
	t.Helper()
	known := map[string]bool{}
	for _, id := range append(append([]string{}, listed...), aliases...) {
		known[id] = true
	}
	target, _ := url.Parse(inner.BaseURL())
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	proxy.Transport = &http.Transport{} // not the redirected default below
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models":
			var data []map[string]string
			for _, id := range listed {
				data = append(data, map[string]string{"id": id, "object": "model"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		case strings.HasPrefix(r.URL.Path, "/v1/models/"):
			if !known[strings.TrimPrefix(r.URL.Path, "/v1/models/")] {
				http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"id":"x"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		if !known[req.Model] {
			http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusNotFound)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()

	orig := http.DefaultTransport
	http.DefaultTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	t.Cleanup(func() {
		providers.ForgetCatalogsForTest()
		http.DefaultTransport = orig
	})
}

// runAgentCLI runs `celeste -config e2e agent ...` in-process and returns
// what it printed on stderr.
func runAgentCLI(t *testing.T, ws string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origErr, origName := os.Stderr, configName
	os.Stderr, configName = w, "e2e"
	defer func() { os.Stderr, configName = origErr, origName }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	runAgentCommand([]string{"-goal", "say ok", "-workspace", ws, "-planner=false", "-no-artifacts", "-no-checkpoint", "-auto-approve", "-verbose=false"})
	_ = w.Close()
	return <-done
}

// M8: `celeste agent` end to end. A model the provider says is gone (404)
// is replaced and the run succeeds; an alias the catalog doesn't list but
// the provider answers is kept, with no note.
func TestAgentCommandEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		name, configured, want string
		aliases                []string
		note                   bool
	}{
		{name: "retired model", configured: "gpt-old", want: "gpt-4.1-nano", note: true},
		{name: "unlisted alias", configured: "gpt-alias", want: "gpt-alias", aliases: []string{"gpt-alias"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			writeNamed(t, home, "e2e", `{"api_key":"k","base_url":"http://api.openai.com/v1","model":"`+tc.configured+`","timeout":10}`)
			inner := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"}) // api.openai.com talks Responses (2.0 W8)
			fakeOpenAIDotCom(t, []string{"gpt-4.1-nano", "text-embedding-3-small"}, tc.aliases, inner)

			stderr := runAgentCLI(t, t.TempDir())
			reqs := inner.Requests()
			if len(reqs) == 0 {
				t.Fatalf("no chat request reached the provider; stderr:\n%s", stderr)
			}
			if got := reqs[0].Body["model"]; got != tc.want {
				t.Errorf("model sent = %v, want %s", got, tc.want)
			}
			if has := strings.Contains(stderr, "no longer serves"); has != tc.note {
				t.Errorf("note on stderr = %v, want %v:\n%s", has, tc.note, stderr)
			}
		})
	}
}
