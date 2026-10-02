package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCheckpointClient struct {
	fakeToolLLMClient
	undos   int
	undoErr error
}

func (f *fakeCheckpointClient) UndoLastChange() (string, error) {
	f.undos++
	if f.undoErr != nil {
		return "", f.undoErr
	}
	return "Restored a.txt to its state before change 1.", nil
}

func (f *fakeCheckpointClient) SessionChanges() (string, error) {
	return "Files changed this session:\n  a.txt  +1 -1", nil
}

func (f *fakeCheckpointClient) RewindTo([]string) ([]string, error) { return nil, nil }

func sendCommand(m AppModel, content string) AppModel {
	model, _ := m.Update(SendMessageMsg{Content: content})
	return model.(AppModel)
}

func TestUndoCommandUsesTheCheckpointer(t *testing.T) {
	client := &fakeCheckpointClient{}
	m := sendCommand(NewApp(client), "/undo")
	assert.Equal(t, 1, client.undos)
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Restored a.txt to its state before change 1."))
}

func TestUndoCommandShowsTheError(t *testing.T) {
	client := &fakeCheckpointClient{undoErr: errors.New("no file changes to undo in this session")}
	m := sendCommand(NewApp(client), "/undo")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Undo: no file changes to undo in this session"))
}

func TestDiffCommandUsesTheCheckpointer(t *testing.T) {
	m := sendCommand(NewApp(&fakeCheckpointClient{}), "/diff")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Files changed this session:\n  a.txt  +1 -1"))
}

func TestUndoAndDiffWithoutACheckpointer(t *testing.T) {
	m := sendCommand(NewApp(&fakeToolLLMClient{}), "/undo")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Undo is unavailable in this session."))
	m = sendCommand(m, "/diff")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Diff is unavailable in this session."))
}
