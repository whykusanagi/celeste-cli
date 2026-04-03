package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// tokenBucket implements a simple rate limiter.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newTokenBucket(reqPerMinute int) *tokenBucket {
	rate := float64(reqPerMinute) / 60.0
	return &tokenBucket{
		tokens:     float64(reqPerMinute),
		maxTokens:  float64(reqPerMinute),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}

// sseConnection tracks state for a single SSE client.
type sseConnection struct {
	id     string
	events chan []byte
	bucket *tokenBucket
	done   chan struct{}
}

// serveSSE starts the HTTP server for SSE transport.
func (s *Server) serveSSE(ctx context.Context) error {
	// Load or generate bearer token
	token, err := loadOrCreateToken(s.config.TokenFile)
	if err != nil {
		return fmt.Errorf("token setup: %w", err)
	}

	connections := &sync.Map{} // id -> *sseConnection
	var connCounter int
	var connMu sync.Mutex

	mux := http.NewServeMux()

	// GET /sse -- establish SSE event stream
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		if !validateBearerToken(r, token) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		connMu.Lock()
		connCounter++
		connID := fmt.Sprintf("conn-%d", connCounter)
		connMu.Unlock()

		conn := &sseConnection{
			id:     connID,
			events: make(chan []byte, 64),
			bucket: newTokenBucket(s.config.RateLimit),
			done:   make(chan struct{}),
		}
		connections.Store(connID, conn)
		defer func() {
			connections.Delete(connID)
			close(conn.done)
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send endpoint event so client knows where to POST
		fmt.Fprintf(w, "event: endpoint\ndata: /message?connectionId=%s\n\n", connID)
		flusher.Flush()

		log.Printf("[mcp-server] SSE connection established: %s", connID)

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.Context().Done():
				log.Printf("[mcp-server] SSE connection closed: %s", connID)
				return
			case data := <-conn.events:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	// POST /message -- receive JSON-RPC requests, route responses to SSE stream
	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		if !validateBearerToken(r, token) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		connID := r.URL.Query().Get("connectionId")
		if connID == "" {
			http.Error(w, "missing connectionId", http.StatusBadRequest)
			return
		}

		connVal, ok := connections.Load(connID)
		if !ok {
			http.Error(w, "unknown connection", http.StatusNotFound)
			return
		}
		conn := connVal.(*sseConnection)

		// Rate limit check
		if !conn.bucket.allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024)) // 1MB max
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}

		var req mcp.Request
		if err := json.Unmarshal(body, &req); err != nil {
			errResp := s.errorResponse(0, -32700, "parse error", nil)
			data, _ := json.Marshal(errResp)
			conn.events <- data
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Dispatch the request
		resp, err := s.dispatch(r.Context(), &req)
		if err != nil {
			errResp := s.errorResponse(req.ID, -32603, err.Error(), nil)
			data, _ := json.Marshal(errResp)
			conn.events <- data
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if resp != nil {
			data, _ := json.Marshal(resp)
			conn.events <- data
		}

		w.WriteHeader(http.StatusAccepted)
	})

	bindAddr := fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.Port)
	if s.config.Remote && s.config.BindAddr == "127.0.0.1" {
		bindAddr = fmt.Sprintf("0.0.0.0:%d", s.config.Port)
	}

	httpServer := &http.Server{
		Addr:    bindAddr,
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[mcp-server] SSE transport starting on %s", bindAddr)
	log.Printf("[mcp-server] Bearer token: %s", token)

	// Start server in a goroutine and wait for context cancellation
	errCh := make(chan error, 1)
	go func() {
		var listenErr error
		if s.config.CertFile != "" && s.config.KeyFile != "" {
			log.Printf("[mcp-server] mTLS enabled (cert=%s, key=%s)", s.config.CertFile, s.config.KeyFile)
			listenErr = httpServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
		} else {
			if s.config.Remote {
				log.Printf("[mcp-server] WARNING: remote mode without TLS -- traffic is unencrypted")
			}
			listenErr = httpServer.ListenAndServe()
		}
		if listenErr != nil && listenErr != http.ErrServerClosed {
			errCh <- listenErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Printf("[mcp-server] SSE transport shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// validateBearerToken checks the Authorization header against the expected token.
func validateBearerToken(r *http.Request, expected string) bool {
	auth := r.Header.Get("Authorization")
	if len(auth) < 7 || auth[:7] != "Bearer " {
		return false
	}
	return auth[7:] == expected
}
