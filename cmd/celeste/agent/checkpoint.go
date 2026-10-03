package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

type CheckpointStore struct {
	runsDir string
}

type RunSummary struct {
	RunID     string
	Goal      string
	Status    string
	UpdatedAt time.Time
	Turn      int
	ToolCalls int
}

func NewCheckpointStore(baseDir string) (*CheckpointStore, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".celeste")
	}

	runsDir := filepath.Join(baseDir, "agent", "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return nil, fmt.Errorf("create checkpoint dir: %w", err)
	}

	return &CheckpointStore{runsDir: runsDir}, nil
}

func (s *CheckpointStore) Save(state *RunState) error {
	if state == nil {
		return fmt.Errorf("run state is nil")
	}
	state.UpdatedAt = time.Now()
	state.Messages = withCurrentBlocksOnly(state.Messages)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run state: %w", err)
	}

	path := filepath.Join(s.runsDir, state.RunID+".json")
	// Atomic, so --resume never reads a torn checkpoint; 0600 like sessions,
	// since it holds the run's whole transcript.
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

func (s *CheckpointStore) Load(runID string) (*RunState, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}

	path := filepath.Join(s.runsDir, runID+".json")
	if !pathutil.Within(s.runsDir, path) {
		return nil, fmt.Errorf("invalid run id: %s", runID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
	// Runs checkpointed before 2.0 may hold uncapped tool results (F3).
	state.Messages = tui.CapLoadedToolResults(state.Messages, ctxmgr.DefaultMaxToolResultBytes)
	return &state, nil
}

func (s *CheckpointStore) List(limit int) ([]RunSummary, error) {
	files, err := filepath.Glob(filepath.Join(s.runsDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}

	summaries := make([]RunSummary, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var state RunState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		summaries = append(summaries, RunSummary{
			RunID:     state.RunID,
			Goal:      state.Goal,
			Status:    state.Status,
			UpdatedAt: state.UpdatedAt,
			Turn:      state.Turn,
			ToolCalls: state.ToolCallCount,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries, nil
}

// withCurrentBlocksOnly drops provider blocks that no longer describe their
// message: the zero value a damaged checkpoint loads as, and blocks an edit
// made inert. They would never replay, so they are not written (2.0 F3).
// Copy-on-write: a history slice someone else holds is not modified.
func withCurrentBlocksOnly(msgs []tui.ChatMessage) []tui.ChatMessage {
	out := msgs
	copied := false
	for i := range msgs {
		if msgs[i].ProviderBlocks == nil || tui.CurrentBlocks(msgs[i]) != nil {
			continue
		}
		if !copied {
			out = append([]tui.ChatMessage(nil), msgs...)
			copied = true
		}
		out[i].ProviderBlocks = nil
	}
	return out
}
