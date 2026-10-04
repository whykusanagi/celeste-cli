package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PlanModer is a client with plan mode (2.0 W4e): read-only tools until the
// user approves a plan the model submits with submit_plan. The chat's
// adapter implements it; approval clears the mode from the run's goroutine,
// so the app reads PlanMode when it needs it instead of caching it.
type PlanModer interface {
	SetPlanMode(on bool, goal string)
	PlanMode() bool
	// ShowPlan renders the approved plan (.celeste/plan.json) with each
	// step's todo status, or says there is none.
	ShowPlan() string
}

// PlanUntracker is a client that follows an approved plan's progress
// (#325). The app calls UntrackPlan when it replaces the conversation
// (/clear, /session new, resume or clear): the new chat is not carrying
// out the old plan, so its progress reminder stops.
type PlanUntracker interface {
	UntrackPlan()
}

// untrackPlan stops the client's plan progress reminder, if it has one.
func (m AppModel) untrackPlan() {
	if pu, ok := m.llmClient.(PlanUntracker); ok {
		pu.UntrackPlan()
	}
}

// PlanModeInstruction precedes each prompt while plan mode is on, as a
// hidden user message (ruling 9).
const PlanModeInstruction = "Plan mode: investigate with read-only tools, then call submit_plan with concrete, ordered steps. Do not change files or run commands until the plan is approved."

const (
	planOnText         = "Plan mode on: read-only tools until you approve a plan (/plan off to leave)"
	planOffText        = "Plan mode off: all tools are available again."
	planAlreadyOnText  = "Plan mode is already on."
	planWasOnText      = "Plan mode was on when this session was saved; /plan to resume it."
	planCancelGoneText = "/plan cancel is gone: use /plan off (delete .celeste/plan.json to drop an approved plan)"
	planUsage          = "Usage:\n  /plan           Enter plan mode (read-only tools until you approve a plan)\n  /plan <goal>    Enter plan mode and start planning <goal>\n  /plan off       Leave plan mode\n  /plan show      Show the approved plan and its todo status"
)

// isPlanInstruction is the hidden plan-mode instruction.
func isPlanInstruction(m ChatMessage) bool {
	hidden, _ := m.Metadata["hidden"].(bool)
	return hidden && m.Role == "user" && m.Content == PlanModeInstruction
}

// savedInPlanMode reports whether a restored history ends mid-plan: its
// last prompt carries the plan-mode instruction and no approval follows.
func savedInPlanMode(msgs []ChatMessage) bool {
	last := -1
	for i, msg := range msgs {
		if hidden, _ := msg.Metadata["hidden"].(bool); msg.Role == "user" && !hidden {
			last = i
		}
	}
	if last < 1 || !isPlanInstruction(msgs[last-1]) {
		return false
	}
	for _, msg := range msgs[last+1:] {
		if msg.Role == "tool" && strings.HasPrefix(msg.Content, planApprovedPrefix) {
			return false
		}
	}
	return true
}

// planApprovedPrefix starts submit_plan's result for an approved plan.
const planApprovedPrefix = "Plan approved:"

// withPlanResumeNote tells the user a restored session was saved in plan
// mode, which a new process starts with off.
func (m AppModel) withPlanResumeNote(msgs []ChatMessage) AppModel {
	if _, ok := m.planModer(); ok && !m.planModeOn() && savedInPlanMode(msgs) {
		m.chat = m.chat.AddSystemMessage(planWasOnText)
	}
	return m
}

// planModer is the client's plan mode, if it has one.
func (m AppModel) planModer() (PlanModer, bool) {
	pm, ok := m.llmClient.(PlanModer)
	return pm, ok
}

// planModeOn reports whether the client is in plan mode.
func (m AppModel) planModeOn() bool {
	pm, ok := m.planModer()
	return ok && pm.PlanMode()
}

// withPlanInstruction adds the hidden plan-mode instruction before the
// prompt about to be added, while plan mode is on.
func (m AppModel) withPlanInstruction() AppModel {
	if m.planModeOn() {
		m.chat = m.chat.AddHiddenUserMessage(PlanModeInstruction)
	}
	return m
}

// planCommand is /plan (ruling 11): on; on with a goal sent as the prompt;
// off; show.
func (m AppModel) planCommand(args []string) (tea.Model, tea.Cmd) {
	pm, ok := m.planModer()
	if !ok {
		m.chat = m.chat.AddSystemMessage("Plan mode is unavailable in this session.")
		return m, nil
	}
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "off":
			if !pm.PlanMode() {
				m.chat = m.chat.AddSystemMessage("Plan mode is already off.")
				return m, nil
			}
			pm.SetPlanMode(false, "")
			m.chat = m.chat.AddSystemMessage(planOffText)
			return m, nil
		case "show":
			m.chat = m.chat.AddSystemMessage(pm.ShowPlan())
			return m, nil
		case "help":
			m.chat = m.chat.AddSystemMessage(planUsage)
			return m, nil
		case "cancel":
			// 1.x's way out; typed from habit it must not plan "cancel".
			m.chat = m.chat.AddSystemMessage(planCancelGoneText)
			return m, nil
		}
	}
	goal := strings.TrimSpace(strings.Join(args, " "))
	if goal == "" && pm.PlanMode() {
		// Keep the goal an earlier /plan <goal> set.
		m.chat = m.chat.AddSystemMessage(planAlreadyOnText)
		return m, nil
	}
	pm.SetPlanMode(true, goal)
	m.chat = m.chat.AddSystemMessage(planOnText)
	if goal == "" {
		return m, nil
	}
	return m.sendPlanPrompt(goal)
}

// sendPlanPrompt sends /plan <goal>'s goal as the prompt, after the hidden
// plan-mode instruction, and keeps "Planning..." until the reply streams.
func (m AppModel) sendPlanPrompt(goal string) (tea.Model, tea.Cmd) {
	m = m.withPlanInstruction()
	m.chat = m.chat.AddUserMessage(goal)
	m.persistSession()
	var turnCmd tea.Cmd
	m, turnCmd = m.startTurn()
	m.status = m.status.SetText("Planning...")
	m.planning = m.turn != nil
	if m.turn == nil {
		return m, turnCmd
	}
	return m, tea.Batch(turnCmd, m.restartTick(typingTickInterval*2))
}
