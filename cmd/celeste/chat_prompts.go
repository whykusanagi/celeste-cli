package main

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// permissionPrompt is the chat's tools.PromptFunc: it shows the permission
// modal through send (p.Send) and blocks until it is answered. It runs off
// the Update goroutine (the loop's Gate). The request names the run that
// asked and carries that run's Done (req.Context), so the chat answers a
// request whose run has ended instead of showing it (2.0 F2e).
func permissionPrompt(send func(tea.Msg)) tools.PromptFunc {
	return func(req tools.PermissionRequest) tools.PermissionResponse {
		ch := make(chan tui.PermissionResponse, 1)
		msg := tui.PermissionRequestMsg{ToolName: req.ToolName, InputSummary: req.InputSummary, RiskLevel: req.RiskLevel, Response: ch}
		if req.Context != nil {
			msg.Owner, msg.Done = tui.RunOwnerFrom(req.Context), req.Context.Done()
		}
		send(msg)
		r, ok := <-ch
		if !ok {
			return tools.PermissionResponse{Decision: "deny"}
		}
		return tools.PermissionResponse{Decision: r.Decision, Pattern: r.Pattern}
	}
}

// askPrompt is the chat's tools.AskFunc: the ask modal, the same way. It
// also gives up when ctx ends.
func askPrompt(send func(tea.Msg)) tools.AskFunc {
	return func(ctx context.Context, req tools.AskRequest) (tools.AskResponse, error) {
		ch := make(chan tui.AskResponseMsg, 1)
		opts := make([]tui.AskOption, 0, len(req.Options))
		for _, o := range req.Options {
			opts = append(opts, tui.AskOption{Label: o.Label, Description: o.Description})
		}
		send(tui.AskRequestMsg{Question: req.Question, Options: opts, MultiSelect: req.MultiSelect, Response: ch,
			Owner: tui.RunOwnerFrom(ctx), Done: ctx.Done()})
		select {
		case r, ok := <-ch:
			if !ok {
				return tools.AskResponse{Cancelled: true}, nil
			}
			return tools.AskResponse{Selected: r.Selected, Cancelled: r.Cancelled}, nil
		case <-ctx.Done():
			return tools.AskResponse{Cancelled: true}, ctx.Err()
		}
	}
}
