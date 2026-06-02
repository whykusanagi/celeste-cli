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
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id,omitempty"` // user-assigned ID for DAG dependencies
	Name         string    `json:"name"`              // display name (e.g., "火 hi")
	Element      string    `json:"element"`           // english element (e.g., "fire")
	Goal         string    `json:"goal"`
	Workspace    string    `json:"workspace"`
	Status       string    `json:"status"`               // "waiting", "running", "completed", "failed"
	DependsOn    []string  `json:"depends_on,omitempty"` // task_ids that must complete first
	Result       string    `json:"result"`
	Error        string    `json:"error,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	Turns        int       `json:"turns"`
	CheckpointID string    `json:"checkpoint_id,omitempty"` // run id to resume from on failure
}

// DAGEntry is a queued subagent waiting for dependencies to clear.
type DAGEntry struct {
	Run             *SubagentRun
	Goal            string // the full goal including persona overrides
	Workspace       string
	TurnCb          TurnCallback
	MaxTurns        int
	IsolateWorktree bool
	ResultCh        chan *SubagentRun // result sent here when complete
}

// StaggerDelay is the configurable pause between concurrent subagent
// launches to avoid hitting provider rate limits. Zero means no delay.
// Default 500ms — safe for xAI's grok-build-0.1 at 6 concurrent agents.
var StaggerDelay = 500 * time.Millisecond

// Manager handles subagent lifecycle and execution.
type Manager struct {
	cfg       *config.Config
	workspace string
	mu        sync.Mutex
	mergeMu   sync.Mutex // serializes git merge operations across concurrent subagents
	runs      map[string]*SubagentRun
	counter   int
	isChild   bool        // true if this manager is inside a subagent (blocks recursion)
	dagQueue  []*DAGEntry // tasks waiting for dependencies
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

// SpawnOptions holds optional parameters for Spawn.
type SpawnOptions struct {
	TaskID          string       // user-assigned task ID for DAG references
	DependsOn       []string     // task IDs that must complete before this starts
	TurnCb          TurnCallback // nested progress callback
	MaxTurns        int          // 0 = default (20)
	IsolateWorktree bool         // run this subagent in its own git worktree (#32)
}

// Spawn creates and runs a subagent with the given goal. It blocks until the
// subagent completes or the context is cancelled.
func (m *Manager) Spawn(ctx context.Context, goal string, workspace string, turnCb ...TurnCallback) (*SubagentRun, error) {
	opts := SpawnOptions{}
	if len(turnCb) > 0 {
		opts.TurnCb = turnCb[0]
	}
	return m.SpawnWithOptions(ctx, goal, workspace, opts)
}

// SpawnWithOptions creates and runs a subagent with full options including
// DAG dependencies. If depends_on contains task IDs that haven't completed
// yet, the subagent blocks in "waiting" state until all dependencies clear,
// then auto-starts.
func (m *Manager) SpawnWithOptions(ctx context.Context, goal string, workspace string, opts SpawnOptions) (*SubagentRun, error) {
	if m.isChild {
		return nil, fmt.Errorf("recursive subagent spawning is not allowed")
	}

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
		TaskID:    opts.TaskID,
		Name:      name,
		Element:   element,
		Goal:      goal,
		Workspace: workspace,
		Status:    "running",
		DependsOn: opts.DependsOn,
		StartedAt: time.Now(),
	}

	if opts.TaskID != "" {
		// Register by task ID so dependencies can reference it
		m.runs[opts.TaskID] = run
	}
	m.runs[id] = run

	// Auto-detect dependencies from goal text.
	// Release lock briefly so peer agents in the same batch can register,
	// then re-acquire and scan for references to their task_ids.
	if len(opts.DependsOn) == 0 && opts.TaskID != "" {
		m.mu.Unlock()
		time.Sleep(200 * time.Millisecond) // let batch peers register
		m.mu.Lock()

		seen := make(map[string]bool)
		for key, peer := range m.runs {
			if peer.TaskID == "" || peer.TaskID != key || peer.TaskID == opts.TaskID {
				continue
			}
			if seen[peer.TaskID] {
				continue
			}
			if containsWholeWord(goal, peer.TaskID) || (peer.Element != "" && containsWholeWord(goal, peer.Element)) {
				opts.DependsOn = append(opts.DependsOn, peer.TaskID)
				run.DependsOn = append(run.DependsOn, peer.TaskID)
				seen[peer.TaskID] = true
			}
		}
		// DAG auto-detect complete (dependencies logged via TUI progress events)
	}

	// Check if dependencies are met
	if len(opts.DependsOn) > 0 {
		unmet := m.unmetDependencies(opts.DependsOn)
		if len(unmet) > 0 {
			run.Status = "waiting"
			resultCh := make(chan *SubagentRun, 1)
			m.dagQueue = append(m.dagQueue, &DAGEntry{
				Run:             run,
				Goal:            goal,
				Workspace:       workspace,
				TurnCb:          opts.TurnCb,
				MaxTurns:        opts.MaxTurns,
				IsolateWorktree: opts.IsolateWorktree,
				ResultCh:        resultCh,
			})
			m.mu.Unlock()

			// Block until dependencies clear and the entry is executed
			// DAG waiting (visible via TUI tool progress)
			select {
			case completed := <-resultCh:
				// DAG unblocked
				return completed, nil
			case <-ctx.Done():
				// DAG cancelled — tool context expired
				m.mu.Lock()
				run.Status = "failed"
				run.Error = "cancelled while waiting for dependencies"
				run.EndedAt = time.Now()
				m.mu.Unlock()
				return run, ctx.Err()
			}
		}
	}
	m.mu.Unlock()

	return m.executeSubagent(ctx, run, goal, workspace, opts.TurnCb, opts.MaxTurns, opts.IsolateWorktree)
}

// buildAgentOptions constructs the agent runner options shared by spawn and
// resume so the two paths can't drift. maxTurns <= 0 falls back to the
// default of 20. The options set are Workspace, MaxTurns, Verbose, and the
// OnTurnStats callback wired from turnCb (nil turnCb → no callback).
func (m *Manager) buildAgentOptions(workspace string, maxTurns int, turnCb TurnCallback) agent.Options {
	if maxTurns <= 0 {
		maxTurns = 20
	}
	opts := agent.Options{
		Workspace: workspace,
		MaxTurns:  maxTurns,
		Verbose:   false,
	}
	if turnCb != nil {
		cb := turnCb
		opts.OnTurnStats = func(stats agent.TurnStats) {
			toolName := ""
			if len(stats.ToolCalls) > 0 {
				toolName = strings.Join(stats.ToolCalls, ", ")
			}
			cb(stats.Turn, stats.MaxTurns, toolName)
		}
	}
	return opts
}

// executeSubagent runs the actual agent loop for a SubagentRun. Called from
// both the direct SpawnWithOptions path and from drainDAGQueue goroutines.
// When isolate is true, a dedicated git worktree is created under workspace,
// the subagent runs there, and on success the branch is merged back. The
// worktree is always removed when done (defer). run.Workspace is left pointing
// at the durable repo (workspace) — not the ephemeral worktree path — so that
// a future Resume call finds a stable directory even after the worktree is
// cleaned up.
func (m *Manager) executeSubagent(ctx context.Context, run *SubagentRun, goal string, workspace string, turnCb TurnCallback, maxTurns int, isolate bool) (*SubagentRun, error) {
	// Build the subagent goal with recursion marker so child agents
	// cannot spawn further subagents.
	markedGoal := fmt.Sprintf("%s %s", recursionMarker, goal)

	// Resolve the execution workspace. When isolation is requested, create a
	// dedicated git worktree. The element name (e.g. "fire") is used as the
	// directory name; fall back to the run ID if element is empty or contains
	// characters that would be unsafe in a path.
	execWorkspace := workspace
	var wt *Worktree
	if isolate {
		wtName := run.Element
		if wtName == "" {
			wtName = run.ID
		}
		w, err := AddWorktree(workspace, wtName)
		if err != nil {
			run.Status = "failed"
			run.Error = fmt.Sprintf("worktree setup: %v", err)
			run.EndedAt = time.Now()
			return run, err
		}
		wt = w
		execWorkspace = w.Path
		defer func() {
			if run.Status == "completed" {
				m.mergeMu.Lock()
				mErr := MergeWorktree(workspace, wt)
				m.mergeMu.Unlock()
				if mErr != nil {
					// Keep run result intact; note merge failure in the error field.
					run.Error = strings.TrimSpace(run.Error + " (worktree merge failed: " + mErr.Error() + ")")
				}
			}
			_ = RemoveWorktree(workspace, wt)
		}()
	}

	// Stagger concurrent launches based on currently running agents
	// to avoid rate limiting. Uses active count, not total-ever count,
	// so later batches don't get penalized by earlier completions.
	m.mu.Lock()
	activeCount := 0
	for _, r := range m.runs {
		if r.Status == "running" {
			activeCount++
		}
	}
	m.mu.Unlock()
	if StaggerDelay > 0 && activeCount > 1 {
		delay := StaggerDelay * time.Duration(activeCount-1)
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
	// Use execWorkspace (worktree path when isolated, otherwise workspace) for
	// the actual agent run. run.Workspace retains the durable repo path.
	agentOpts := m.buildAgentOptions(execWorkspace, maxTurns, turnCb)

	runner, err := agent.NewRunner(m.cfg, agentOpts, &outBuf, &errBuf)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		run.EndedAt = time.Now()
		return run, fmt.Errorf("create subagent: %w", err)
	}
	defer runner.Close()

	state, err := runner.RunGoal(ctx, markedGoal)

	m.mu.Lock()
	run.EndedAt = time.Now()

	// Always capture the checkpoint id so the caller can resume on failure.
	if state != nil {
		run.CheckpointID = state.RunID
	}

	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		if state != nil {
			run.Turns = state.Turn
		}
		// Capture partial progress — whatever the subagent accomplished
		// before failing. This prevents total work loss on timeout/network errors.
		if state != nil && state.LastAssistantResponse != "" {
			run.Result = fmt.Sprintf("[Partial result — failed after %d turns: %s]\n\n%s",
				state.Turn, err.Error(), state.LastAssistantResponse)
		}
		m.mu.Unlock()
		// Still drain — other tasks may be unblocked by earlier completions
		drainCtx2 := context.Background()
		m.drainDAGQueue(drainCtx2)
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
	// Truncate oversized results to avoid blowing up the parent's context
	const maxResultChars = 100_000
	if len(run.Result) > maxResultChars {
		run.Result = run.Result[:maxResultChars] + "\n\n[Result truncated at 100k chars]"
	}
	m.mu.Unlock()

	// Drain the DAG queue — this task's completion may unblock waiting entries.
	// Use a fresh context. DO NOT defer cancel — the drain goroutines need
	// the context to stay alive after this function returns.
	drainCtx := context.Background()
	m.drainDAGQueue(drainCtx)

	return run, nil
}

// unmetDependencies returns the task IDs from deps that haven't completed.
// Must be called with m.mu held.
func (m *Manager) unmetDependencies(deps []string) []string {
	var unmet []string
	for _, depID := range deps {
		run, ok := m.runs[depID]
		if !ok || run.Status != "completed" {
			unmet = append(unmet, depID)
		}
	}
	return unmet
}

// drainDAGQueue checks all waiting entries and starts any whose
// dependencies are now fully met. Called after every task completion.
func (m *Manager) drainDAGQueue(ctx context.Context) {
	m.mu.Lock()
	var stillWaiting []*DAGEntry
	var ready []*DAGEntry
	for _, entry := range m.dagQueue {
		unmet := m.unmetDependencies(entry.Run.DependsOn)
		if len(unmet) == 0 {
			ready = append(ready, entry)
		} else {
			stillWaiting = append(stillWaiting, entry)
		}
	}
	m.dagQueue = stillWaiting
	m.mu.Unlock()

	// Execute ready entries in goroutines
	for _, entry := range ready {
		go func(e *DAGEntry) {
			// Inject dependency results into the goal context
			m.mu.Lock()
			var depContext strings.Builder
			for _, depID := range e.Run.DependsOn {
				if depRun, ok := m.runs[depID]; ok && depRun.Status == "completed" {
					depContext.WriteString(fmt.Sprintf("[DEPENDENCY RESULT: %s (%s)]\n%s\n[END DEPENDENCY]\n\n",
						depRun.Name, depID, depRun.Result))
				}
			}
			e.Run.Status = "running"
			e.Run.StartedAt = time.Now()
			m.mu.Unlock()

			enrichedGoal := e.Goal
			if depContext.Len() > 0 {
				enrichedGoal = depContext.String() + enrichedGoal
			}

			// Run the actual subagent (reuse the execution path)
			result, _ := m.executeSubagent(ctx, e.Run, enrichedGoal, e.Workspace, e.TurnCb, e.MaxTurns, e.IsolateWorktree)
			e.ResultCh <- result
		}(entry)
	}
}

// GetRun returns a subagent run by ID.
func (m *Manager) GetRun(id string) (*SubagentRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	return run, ok
}

// Resume continues a previously-failed subagent from its last checkpoint.
// checkpointID is the RunID returned by RunGoal and stored in SubagentRun.CheckpointID.
// It constructs a runner with the same options as executeSubagent so the
// resumed run shares the same workspace, turn limits, and callback wiring.
func (m *Manager) Resume(ctx context.Context, checkpointID string, turnCb TurnCallback) (*SubagentRun, error) {
	var outBuf, errBuf bytes.Buffer

	// Resume in the same workspace the subagent originally ran in (e.g. an
	// isolated worktree), falling back to the manager default if the original
	// run isn't in memory (e.g. after a process restart).
	workspace := m.workspace
	m.mu.Lock()
	for _, r := range m.runs {
		if r.CheckpointID == checkpointID && r.Workspace != "" {
			workspace = r.Workspace
			break
		}
	}
	m.mu.Unlock()

	agentOpts := m.buildAgentOptions(workspace, 0, turnCb)

	runner, err := agent.NewRunner(m.cfg, agentOpts, &outBuf, &errBuf)
	if err != nil {
		return nil, fmt.Errorf("create runner for resume: %w", err)
	}
	defer runner.Close()

	run := &SubagentRun{
		ID:           checkpointID,
		CheckpointID: checkpointID,
		StartedAt:    time.Now(),
		Status:       "running",
	}

	// Register the resumed run so ListRuns/GetRun reflect it.
	m.mu.Lock()
	m.runs[run.ID] = run
	m.mu.Unlock()

	state, err := runner.Resume(ctx, checkpointID)
	run.EndedAt = time.Now()
	if state != nil {
		// CheckpointID stays as set in the struct literal (checkpointID); do
		// not overwrite it with state.RunID — the id is already known here.
		run.Result = state.LastAssistantResponse
		run.Turns = state.Turn
	}
	// Apply the same outBuf fallback as executeSubagent so a resumed run
	// whose LastAssistantResponse is empty still surfaces captured output.
	if run.Result == "" && outBuf.Len() > 0 {
		run.Result = outBuf.String()
	}
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		return run, fmt.Errorf("resume subagent: %w", err)
	}
	run.Status = "completed"
	return run, nil
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
