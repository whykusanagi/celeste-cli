package main

import (
	"errors"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
)

var errCheckpointsOff = errors.New("file checkpoints are off in this session")

// UndoLastChange implements tui.Checkpointer (/undo): the newest change in
// this session is put back, and repeating it walks back further. When the
// file changed after that change (outside celeste's write tools), the
// first /undo leaves it alone and says so; /undo again overwrites it.
func (a *TUIClientAdapter) UndoLastChange() (string, error) {
	if a.snapshots == nil {
		return "", errCheckpointsOff
	}
	if entries := a.snapshots.Entries(); len(entries) > 0 {
		last := entries[len(entries)-1]
		confirmed := a.undoConfirm != nil && checkpoints.SameEntry(*a.undoConfirm, last)
		a.undoConfirm = nil
		if mod, changed := checkpoints.ModifiedAfter(last); changed && !confirmed {
			a.undoConfirm = &last
			return "", fmt.Errorf("%s changed after celeste's last change to it (modified %s); /undo again to overwrite it with its state before that change",
				checkpoints.DisplayPath(a.workspace, last.Path), mod.Local().Format("2006-01-02 15:04:05"))
		}
	}
	e, err := a.snapshots.RevertLast()
	if err != nil {
		return "", err
	}
	name := checkpoints.DisplayPath(a.workspace, e.Path)
	if e.Backup == "" {
		return fmt.Sprintf("Undid the creation of %s (deleted it).", name), nil
	}
	return fmt.Sprintf("Restored %s to its state before change %d.", name, e.Version), nil
}

// SessionChanges implements tui.Checkpointer (/diff): every file this
// session changed, against its state before the first change.
func (a *TUIClientAdapter) SessionChanges() (string, error) {
	if a.snapshots == nil {
		return "", errCheckpointsOff
	}
	changes, err := a.snapshots.ComputeDiff()
	if err != nil {
		return "", err
	}
	return checkpoints.FormatChanges(changes, a.workspace), nil
}
