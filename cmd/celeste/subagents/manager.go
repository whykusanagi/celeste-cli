// Package subagents provides foreground subagent spawning for task delegation.
// A subagent is a fully independent agent loop with its own LLM client, tool
// registry, and message history. It inherits the parent's config, persona,
// grimoire, and permissions but runs in isolation. For v1.8, subagents run
// in the foreground (blocking) with no inter-agent messaging.
package subagents

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// recursionMarker is injected into subagent messages to detect and block
// recursive spawning. If a subagent's goal contains this marker, the spawn
// is rejected.
const recursionMarker = "[celeste-subagent]"

// elementNames are the named identities for the first 6 subagents,
// using Japanese elemental kanji from the corruption-theme aesthetic.
// After 6, falls back to numbered names (七号, 八号, ...).
var elementNames = []struct {
	Kanji   string // display character
	Romaji  string // romanized name
	Element string // english meaning
}{
	{"地", "chi", "earth"},
	{"火", "hi", "fire"},
	{"水", "mizu", "water"},
	{"光", "hikari", "light"},
	{"闇", "yami", "dark"},
	{"風", "kaze", "wind"},
}

// SubagentRun tracks the state and result of a spawned subagent.
type SubagentRun struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`      // display name (e.g., "火 hi")
	Element   string    `json:"element"`   // english element (e.g., "fire")
	Goal      string    `json:"goal"`
	Workspace string    `json:"workspace"`
	Status    string    `json:"status"` // "running", "completed", "failed"
	Result    string    `json:"result"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Turns     int       `json:"turns"`
}

// StaggerDelay is the configurable pause between concurrent subagent
// launches to avoid hitting provider rate limits. Zero means no delay.
// Default 500ms — safe for xAI's grok-4-1-fast at 6 concurrent agents.
var StaggerDelay = 500 * time.Millisecond

// Manager handles subagent lifecycle and execution.
type Manager struct {
	cfg       *config.Config
	workspace string
	mu        sync.Mutex
	runs      map[string]*SubagentRun
	counter   int
	isChild   bool // true if this manager is inside a subagent (blocks recursion)
}

// NewManager creates a subagent manager. Pass isChild=true when the manager
// itself is running inside a subagent to block recursive spawning.
func NewManager(cfg *config.Config, workspace string, isChild bool) *Manager {
	return &Manager{
		cfg:       cfg,
		workspace: workspace,
		runs:      make(map[string]*SubagentRun),
		isChild:   isChild,
	}
}

// TurnCallback is called on each subagent turn so the parent can
// display nested progress. turn is 1-indexed, toolName is the tool
// being called on this turn (empty if the turn is a text response).
type TurnCallback func(turn int, maxTurns int, toolName string)

// Spawn creates and runs a subagent with the given goal. It blocks until the
// subagent completes or the context is cancelled. The optional turnCb streams
// per-turn activity to the caller for nested TUI progress display.
func (m *Manager) Spawn(ctx context.Context, goal string, workspace string, turnCb ...TurnCallback) (*SubagentRun, error) {
	if m.isChild {
		return nil, fmt.Errorf("recursive subagent spawning is not allowed")
	}

	// Check for recursion marker in the goal itself (defense in depth)
	if strings.Contains(goal, recursionMarker) {
		return nil, fmt.Errorf("recursive subagent spawning detected")
	}

	if workspace == "" {
		workspace = m.workspace
	}

	m.mu.Lock()
	m.counter++
	idx := m.counter - 1
	id := fmt.Sprintf("sub-%d-%d", time.Now().Unix(), m.counter)

	// Assign element name from the kanji table
	var name, element string
	if idx < len(elementNames) {
		e := elementNames[idx]
		name = fmt.Sprintf("%s %s", e.Kanji, e.Romaji)
		element = e.Element
	} else {
		name = fmt.Sprintf("第%d号", m.counter)
		element = fmt.Sprintf("agent-%d", m.counter)
	}

	run := &SubagentRun{
		ID:        id,
		Name:      name,
		Element:   element,
		Goal:      goal,
		Workspace: workspace,
		Status:    "running",
		StartedAt: time.Now(),
	}
	m.runs[id] = run
	m.mu.Unlock()

	// Build the subagent goal with recursion marker so child agents
	// cannot spawn further subagents.
	markedGoal := fmt.Sprintf("%s %s", recursionMarker, goal)

	// Stagger concurrent launches to avoid rate limiting
	if StaggerDelay > 0 && m.counter > 1 {
		delay := StaggerDelay * time.Duration(m.counter-1)
		if delay > 3*time.Second {
			delay = 3 * time.Second // cap at 3s max stagger
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			run.Status = "failed"
			run.Error = "cancelled during stagger delay"
			run.EndedAt = time.Now()
			return run, ctx.Err()
		}
	}

	var outBuf, errBuf bytes.Buffer
	opts := agent.Options{
		Workspace: workspace,
		MaxTurns:  20,
		Verbose:   false,
	}

	// Wire turn callback via OnTurnStats — the agent runner fires
	// this after each LLM call so we can forward nested progress
	// to the parent's TUI for real-time subagent visibility.
	if len(turnCb) > 0 && turnCb[0] != nil {
		cb := turnCb[0]
		opts.OnTurnStats = func(stats agent.TurnStats) {
			toolName := ""
			if len(stats.ToolCalls) > 0 {
				toolName = strings.Join(stats.ToolCalls, ", ")
			}
			cb(stats.Turn, stats.MaxTurns, toolName)
		}
	}

	runner, err := agent.NewRunner(m.cfg, opts, &outBuf, &errBuf)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		run.EndedAt = time.Now()
		return run, fmt.Errorf("create subagent: %w", err)
	}
	defer runner.Close()

	state, err := runner.RunGoal(ctx, markedGoal)

	m.mu.Lock()
	defer m.mu.Unlock()

	run.EndedAt = time.Now()

	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		return run, fmt.Errorf("subagent execution: %w", err)
	}

	run.Status = "completed"
	run.Turns = state.Turn

	// Collect the result
	run.Result = state.LastAssistantResponse
	if run.Result == "" && outBuf.Len() > 0 {
		run.Result = outBuf.String()
	}
	if run.Result == "" {
		run.Result = fmt.Sprintf("Subagent completed after %d turns (status: %s)", state.Turn, state.Status)
	}

	return run, nil
}

// GetRun returns a subagent run by ID.
func (m *Manager) GetRun(id string) (*SubagentRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	return run, ok
}

// ListRuns returns all subagent runs, most recent first.
func (m *Manager) ListRuns() []*SubagentRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	runs := make([]*SubagentRun, 0, len(m.runs))
	for _, r := range m.runs {
		runs = append(runs, r)
	}
	// Sort by start time descending
	for i := 0; i < len(runs); i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[j].StartedAt.After(runs[i].StartedAt) {
				runs[i], runs[j] = runs[j], runs[i]
			}
		}
	}
	return runs
}
