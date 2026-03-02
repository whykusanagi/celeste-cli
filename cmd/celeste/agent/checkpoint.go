package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
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

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run state: %w", err)
	}

	path := filepath.Join(s.runsDir, state.RunID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

func (s *CheckpointStore) Load(runID string) (*RunState, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}

	path := filepath.Join(s.runsDir, runID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
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
