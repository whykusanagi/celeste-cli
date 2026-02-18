// Package llm provides the LLM client for Celeste CLI.
// This file contains streaming-specific functionality.
package llm

import (
	"sync"
	"time"
)

// StreamState tracks the state of a streaming response.
type StreamState struct {
	mu             sync.Mutex
	content        string
	chunks         []string
	chunkTimes     []time.Time
	startTime      time.Time
	firstChunkTime time.Time
	isDump         bool
	isComplete     bool
}

// NewStreamState creates a new stream state tracker.
func NewStreamState() *StreamState {
	return &StreamState{
		startTime: time.Now(),
	}
}

// AddChunk adds a chunk to the stream state.
func (s *StreamState) AddChunk(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.chunks = append(s.chunks, content)
	s.chunkTimes = append(s.chunkTimes, now)
	s.content += content

	// Track first chunk time for dump detection
	if len(s.chunks) == 1 {
		s.firstChunkTime = now
	}
}

// GetContent returns the accumulated content.
func (s *StreamState) GetContent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.content
}

// MarkComplete marks the stream as complete.
func (s *StreamState) MarkComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isComplete = true

	// Detect if this was a "dump" (all content at once)
	s.detectDump()
}

// detectDump checks if the response was dumped all at once.
func (s *StreamState) detectDump() {
	if len(s.chunks) == 0 {
		return
	}

	// Calculate first chunk ratio
	firstChunkSize := len(s.chunks[0])
	totalSize := len(s.content)

	if totalSize == 0 {
		return
	}

	ratio := float64(firstChunkSize) / float64(totalSize)
	elapsed := s.firstChunkTime.Sub(s.startTime)

	// If >80% of content in first chunk within 500ms, it's a dump
	s.isDump = ratio > 0.8 && elapsed < 500*time.Millisecond
}

// IsDump returns whether the stream was a dump (all at once).
func (s *StreamState) IsDump() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isDump
}

// IsComplete returns whether the stream is complete.
func (s *StreamState) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isComplete
}

// GetChunkCount returns the number of chunks received.
func (s *StreamState) GetChunkCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks)
}

// GetDuration returns the total stream duration.
func (s *StreamState) GetDuration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.chunkTimes) == 0 {
		return 0
	}

	lastTime := s.chunkTimes[len(s.chunkTimes)-1]
	return lastTime.Sub(s.startTime)
}

// SimulatedStreamConfig holds configuration for simulated streaming.
type SimulatedStreamConfig struct {
	TypingSpeed  int     // Characters per second
	GlitchChance float64 // Chance of corruption effect (0-1)
	MinDelay     time.Duration
	MaxDelay     time.Duration
}

// DefaultSimulatedConfig returns default simulated streaming config.
func DefaultSimulatedConfig() SimulatedStreamConfig {
	return SimulatedStreamConfig{
		TypingSpeed:  40,                     // 40 chars/sec
		GlitchChance: 0.02,                   // 2% chance
		MinDelay:     time.Millisecond * 20,  // 20ms minimum
		MaxDelay:     time.Millisecond * 100, // 100ms maximum
	}
}

// SimulatedStream simulates streaming for dump responses.
type SimulatedStream struct {
	content    string
	currentPos int
	config     SimulatedStreamConfig
	mu         sync.Mutex
	done       bool
}

// NewSimulatedStream creates a new simulated stream.
func NewSimulatedStream(content string, config SimulatedStreamConfig) *SimulatedStream {
	return &SimulatedStream{
		content: content,
		config:  config,
	}
}

// Next returns the next chunk and delay.
func (s *SimulatedStream) Next() (chunk string, delay time.Duration, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentPos >= len(s.content) {
		return "", 0, true
	}

	// Calculate characters for this chunk (1-3 chars)
	charsToSend := 1
	if s.config.TypingSpeed > 30 {
		charsToSend = 2
	}
	if s.config.TypingSpeed > 60 {
		charsToSend = 3
	}

	endPos := s.currentPos + charsToSend
	if endPos > len(s.content) {
		endPos = len(s.content)
	}

	chunk = s.content[s.currentPos:endPos]
	s.currentPos = endPos

	// Calculate delay based on typing speed
	delay = time.Duration(float64(time.Second) / float64(s.config.TypingSpeed) * float64(charsToSend))

	// Clamp to min/max
	if delay < s.config.MinDelay {
		delay = s.config.MinDelay
	}
	if delay > s.config.MaxDelay {
		delay = s.config.MaxDelay
	}

	done = s.currentPos >= len(s.content)
	return chunk, delay, done
}

// GetProgress returns the current progress (0-1).
func (s *SimulatedStream) GetProgress() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.content) == 0 {
		return 1.0
	}
	return float64(s.currentPos) / float64(len(s.content))
}

// Reset resets the simulated stream.
func (s *SimulatedStream) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPos = 0
	s.done = false
}
