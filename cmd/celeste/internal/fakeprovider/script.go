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
	// RedactedThinking adds an Anthropic redacted_thinking block with this
	// data right after the thinking block.
	RedactedThinking string
	// Transformations is raw JSON set as input_transformations on the
	// Anthropic message_start message; "" omits the field.
	Transformations string
	Reasoning       *Reasoning // Responses only: one reasoning item before the output
	// Incomplete ends a Responses stream with response.incomplete and this
	// reason (e.g. "max_output_tokens").
	Incomplete string
	// Fail ends a Responses stream with response.failed carrying this message.
	Fail string
	// Error ends a Responses stream with an "error" event carrying this
	// message.
	Error string
	// FailCode is the code of Fail's response.failed or Error's event
	// (default "server_error").
	FailCode string
	// Truncate closes a Responses stream before any terminal event.
	Truncate bool
	// Drop closes the connection in the middle of the first event after the
	// output items, without ending the chunked body (the client reads
	// io.ErrUnexpectedEOF).
	Drop   bool
	Status int
	Body   string
}

type ToolCall struct{ ID, Name, Args string }

type Thinking struct{ Text, Signature string }

// Reasoning is a Responses reasoning output item.
type Reasoning struct{ ID, Summary, Encrypted string }

// Request is what the client sent: the path, the decoded body, its raw
// bytes and the request headers.
type Request struct {
	Path   string
	Body   map[string]any
	Raw    []byte
	Header http.Header
}

type Server struct {
	srv    *httptest.Server
	prefix string // appended to srv.URL for BaseURL
	write  func(http.ResponseWriter, Turn)
	// writers, when set, picks the writer by request path; a path with no
	// writer is a 404 that consumes no turn.
	writers  map[string]func(http.ResponseWriter, Turn)
	handlers map[string]http.HandlerFunc // fixed answers by path, outside the script
	// reject, when set, vets each scripted request's body before a turn is
	// served: a non-empty answer is a 400 with that error body, and no turn
	// is consumed.
	reject   func(body map[string]any) string
	mu       sync.Mutex
	turns    []Turn
	requests []Request
}

func newServer(t testing.TB, prefix string, write func(http.ResponseWriter, Turn), turns []Turn) *Server {
	t.Helper()
	s := &Server{prefix: prefix, write: write, turns: append([]Turn(nil), turns...)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.requests = append(s.requests, Request{Path: r.URL.Path, Body: body, Raw: raw, Header: r.Header.Clone()})
		if h, ok := s.handlers[r.URL.Path]; ok {
			s.mu.Unlock()
			h(w, r)
			return
		}
		write := s.write
		if s.writers != nil {
			var ok bool
			if write, ok = s.writers[r.URL.Path]; !ok {
				s.mu.Unlock()
				http.Error(w, "fakeprovider: no route for "+r.URL.Path, http.StatusNotFound)
				return
			}
		}
		if s.reject != nil {
			if msg := s.reject(body); msg != "" {
				s.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, msg)
				return
			}
		}
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

// Handle answers every request for path with h, outside the script. The
// request is still recorded.
func (s *Server) Handle(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handlers == nil {
		s.handlers = map[string]http.HandlerFunc{}
	}
	s.handlers[path] = h
}

// BaseURL is the value for llm.Config.BaseURL.
func (s *Server) BaseURL() string { return s.srv.URL + s.prefix }

func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Remaining is the number of scripted turns not yet served.
func (s *Server) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.turns)
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
