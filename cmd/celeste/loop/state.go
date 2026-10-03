package loop

import (
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
)

// RenderState renders the authoritative state a compaction summary
// carries (#200): the workspace todo list (read from its file, which the
// todo tool rewrites on every change), the files this Env's checkpoint
// session changed (F4), relative to the workspace when inside it, and the
// voice rule (the persona is always on, W5).
func (e *Env) RenderState() string {
	var todos []compact.Todo
	if e.Workspace != "" {
		for _, it := range builtin.NewTodoStore(e.Workspace).List() {
			todos = append(todos, compact.Todo{ID: it.ID, Title: it.Title, Status: it.Status})
		}
	}
	var files []string
	if e.Snapshots != nil {
		for _, p := range e.Snapshots.Files() {
			files = append(files, e.displayPath(p))
		}
	}
	return compact.RenderState(todos, files, prompts.VoiceBoundary)
}

// displayPath is p relative to the workspace when it is inside it, also
// when one of the two reaches it through a symlink (macOS's /var is
// /private/var).
func (e *Env) displayPath(p string) string {
	if e.Workspace == "" {
		return p
	}
	if rel := checkpoints.DisplayPath(e.Workspace, p); !filepath.IsAbs(rel) {
		return rel
	}
	ws, err1 := filepath.EvalSymlinks(e.Workspace)
	dir, err2 := filepath.EvalSymlinks(filepath.Dir(p))
	if err1 != nil || err2 != nil {
		return p
	}
	if rel := checkpoints.DisplayPath(ws, filepath.Join(dir, filepath.Base(p))); !filepath.IsAbs(rel) {
		return rel
	}
	return p
}
