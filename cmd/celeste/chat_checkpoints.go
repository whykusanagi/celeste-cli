package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// errCheckpointsOff is the TUI's sentinel, so /rewind can tell "off" from
// a failed restore.
var errCheckpointsOff = tui.ErrCheckpointsOff

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

// rewindWarning is a change /rewind refused to overwrite, and the files'
// states then.
type rewindWarning struct {
	first  checkpoints.Entry
	states map[string]checkpoints.FileState
}

// RewindTo implements tui.Checkpointer (/rewind, 2.0 W4 ruling 5): every
// change from the first one made by any of callIDs onward is undone,
// newest first, and the files restored are returned. When none of the
// calls changed a file nothing happens. As with /undo, when a file is no
// longer as celeste's last change left it, the first /rewind leaves
// everything alone and says so; the same /rewind again, with the files
// still as warned about, overwrites them.
func (a *TUIClientAdapter) RewindTo(callIDs []string) (tui.RewindResult, error) {
	if a.snapshots == nil {
		return tui.RewindResult{}, errCheckpointsOff
	}
	confirm := a.rewindConfirm
	a.rewindConfirm = nil
	undone, err := a.snapshots.RewindToAnyIf(callIDs, func(es []checkpoints.Entry) error {
		// The newest entry of each file is the change the file should
		// still show.
		newest := map[string]checkpoints.Entry{}
		var order []string
		for _, e := range es {
			if _, ok := newest[e.Path]; !ok {
				order = append(order, e.Path)
			}
			newest[e.Path] = e
		}
		states := map[string]checkpoints.FileState{}
		var changed []checkpoints.Entry
		for _, p := range order {
			e := newest[p]
			now, ch, err := checkpoints.Changed(e)
			if err != nil {
				return err
			}
			states[p] = now
			if ch {
				changed = append(changed, e)
			}
		}
		if len(changed) == 0 {
			return nil
		}
		if confirm != nil && checkpoints.SameEntry(confirm.first, es[0]) && sameStates(confirm.states, states) {
			return nil
		}
		a.rewindConfirm = &rewindWarning{first: es[0], states: states}
		var names []string
		for _, e := range changed {
			names = append(names, checkpoints.DisplayPath(a.workspace, e.Path))
		}
		return fmt.Errorf("%s changed after celeste's last change (or celeste cannot tell); the same /rewind again overwrites them with their state before those turns", strings.Join(names, ", "))
	})
	if errors.Is(err, checkpoints.ErrDisabled) {
		return tui.RewindResult{}, fmt.Errorf("%w: %v", errCheckpointsOff, err)
	}
	var res tui.RewindResult
	seen := map[string]bool{}
	for _, e := range undone {
		name := checkpoints.DisplayPath(a.workspace, e.Path)
		if !seen[name] {
			seen[name] = true
			res.Restored = append(res.Restored, name)
		}
	}
	if err == nil {
		res.Partial = a.snapshots.Evicted(callIDs)
	}
	return res, err
}

func sameStates(a, b map[string]checkpoints.FileState) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
