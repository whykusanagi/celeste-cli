package main

import (
	"errors"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
)

var errCheckpointsOff = errors.New("file checkpoints are off in this session")

// undoWarning is a change /undo refused to undo, and the file's state then.
type undoWarning struct {
	entry checkpoints.Entry
	state checkpoints.FileState
}

// UndoLastChange implements tui.Checkpointer (/undo): the newest change in
// this session is put back, and repeating it walks back further. When the
// file is no longer as that change left it (an editor, a formatter, bash,
// a failed write changed it since), the first /undo leaves it alone and
// says so; /undo again, with the file still as warned about, overwrites
// it. The check and the undo run under the store's lock.
func (a *TUIClientAdapter) UndoLastChange() (string, error) {
	if a.snapshots == nil {
		return "", errCheckpointsOff
	}
	confirm := a.undoConfirm
	a.undoConfirm = nil
	e, err := a.snapshots.RevertLastIf(func(e checkpoints.Entry) error {
		now, changed, err := checkpoints.Changed(e)
		if err != nil {
			return err
		}
		if !changed || confirm != nil && checkpoints.SameEntry(confirm.entry, e) && confirm.state == now {
			return nil
		}
		a.undoConfirm = &undoWarning{entry: e, state: now}
		return outsideChangeError(checkpoints.DisplayPath(a.workspace, e.Path), e, "/undo again")
	})
	if err != nil {
		return "", err
	}
	name := checkpoints.DisplayPath(a.workspace, e.Path)
	if e.Backup == "" {
		return fmt.Sprintf("Undid the creation of %s (deleted it).", name), nil
	}
	return fmt.Sprintf("Restored %s to its state before change %d.", name, e.Version), nil
}

// outsideChangeError says that undoing e would lose a change made to name
// since, and how to go ahead anyway (again).
func outsideChangeError(name string, e checkpoints.Entry, again string) error {
	what := fmt.Sprintf("%s changed after celeste's last change to it", name)
	if e.After == nil {
		what = fmt.Sprintf("celeste cannot tell whether %s changed after its last change to it", name)
	}
	if e.Backup == "" {
		if e.After != nil {
			what = fmt.Sprintf("%s changed after celeste created it", name)
		}
		return fmt.Errorf("%s; %s to delete it anyway", what, again)
	}
	return fmt.Errorf("%s; %s to overwrite it with its state before change %d", what, again, e.Version)
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
