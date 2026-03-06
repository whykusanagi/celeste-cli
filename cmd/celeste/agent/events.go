package agent

import "time"

// RunEvent is emitted during agent execution for live status/reporting.
type RunEvent struct {
	RunID     string         `json:"run_id"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Turn      int            `json:"turn,omitempty"`
	Status    string         `json:"status,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// EventSink receives runtime events.
type EventSink func(event RunEvent)

// SetEventSink configures an optional event sink for live run updates.
func (r *Runner) SetEventSink(sink EventSink) {
	r.eventSink = sink
}

func (r *Runner) emitEvent(state *RunState, eventType, message string, data map[string]any) {
	if r == nil || r.eventSink == nil {
		return
	}

	event := RunEvent{
		Type:      eventType,
		Message:   message,
		Timestamp: time.Now(),
		Data:      data,
	}

	if state != nil {
		event.RunID = state.RunID
		event.Turn = state.Turn
		event.Status = state.Status
	}

	r.eventSink(event)
}
