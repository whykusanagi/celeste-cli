// Package fakeprovider serves scripted, streaming LLM provider responses from
// httptest so tests exercise the real llm backends end to end (2.0 F1).
package fakeprovider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Turn is one scripted response. Status != 0 returns that HTTP status with
// Body instead of a stream.
type Turn struct {
	Text      string
	ToolCalls []ToolCall
	Thinking  *Thinking // Anthropic only
	Status    int
	Body      string
}

type ToolCall struct{ ID, Name, Args string }

type Thinking struct{ Text, Signature string }

// Request is what the client sent, decoded.
type Request struct {
	Path string
	Body map[string]any
}

type Server struct {
	srv      *httptest.Server
	prefix   string // appended to srv.URL for BaseURL
	mu       sync.Mutex
	turns    []Turn
	requests []Request
}

func newServer(t testing.TB, prefix string, write func(http.ResponseWriter, Turn), turns []Turn) *Server {
	t.Helper()
	s := &Server{prefix: prefix, turns: append([]Turn(nil), turns...)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.requests = append(s.requests, Request{Path: r.URL.Path, Body: body})
		if len(s.turns) == 0 {
			s.mu.Unlock()
			http.Error(w, "fakeprovider: script exhausted", http.StatusInternalServerError)
			return
		}
		turn := s.turns[0]
		s.turns = s.turns[1:]
		s.mu.Unlock()
		if turn.Status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(turn.Status)
			_, _ = io.WriteString(w, turn.Body)
			return
		}
		write(w, turn)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// BaseURL is the value for llm.Config.BaseURL.
func (s *Server) BaseURL() string { return s.srv.URL + s.prefix }

func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Push appends turns to the script.
func (s *Server) Push(turns ...Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, turns...)
}

// sse writes one server-sent event and flushes it.
func sse(w http.ResponseWriter, event string, data any) {
	b, _ := json.Marshal(data)
	if event != "" {
		_, _ = io.WriteString(w, "event: "+event+"\n")
	}
	_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
