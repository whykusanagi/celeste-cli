package tui

import (
	"fmt"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// fork runs /fork (2.0 W4 ruling 3): save the session, continue in a new
// one holding a copy of its messages (provider blocks included), its
// endpoint, model and workspace. Files are not copied or rewound.
func (m AppModel) fork() AppModel {
	if m.sessionManager == nil || m.currentSession == nil {
		m.chat = m.chat.AddSystemMessage("Fork is unavailable in this session.")
		return m
	}
	m.persistSession()
	var oldID, oldName string
	if sum, ok := m.currentSession.SummarizeRaw().(SessionSummary); ok {
		oldID, oldName = sum.ID, sum.Name
	}
	s, ok := m.sessionManager.NewSession().(Session)
	if !ok {
		m.chat = m.chat.AddSystemMessage("Fork failed: could not create a session.")
		return m
	}
	old := m.currentSession
	s.SetEndpoint(m.endpoint)
	s.SetModel(m.model)
	s.SetModelPinned(m.modelPinned)
	s.SetModelUnverified(m.modelPinned && m.header.modelUnverified)
	s.SetNSFWMode(m.nsfwMode)
	if ws := old.GetWorkspace(); ws != "" {
		s.SetWorkspace(ws)
	}
	s.SetCommandHistory(old.GetCommandHistory())
	// SessionMessagesFromChat builds new messages and tool call slices;
	// provider blocks are never modified in place, so sharing them is a
	// copy.
	s.SetMessagesRaw(SessionMessagesFromChat(m.savedMessages()))
	if oldName == "" {
		oldName = oldID
	}
	s.SetName("fork of " + oldName)
	if err := m.sessionManager.Save(s); err != nil {
		m.chat = m.chat.AddSystemMessage("Fork failed: " + err.Error())
		return m
	}
	m.currentSession = s
	if cs, ok := s.(*config.Session); ok && m.contextTracker != nil {
		m.contextTracker.Session = cs
	}
	var newID string
	if sum, ok := s.SummarizeRaw().(SessionSummary); ok {
		newID = sum.ID
	}
	m.chat = m.chat.AddSystemMessage(fmt.Sprintf(
		"Forked into %s; the original is unchanged (/session resume %s to go back).", newID, oldID))
	return m
}
