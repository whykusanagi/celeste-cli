package acp

import (
	"encoding/json"
	"testing"
)

func TestInitialize(t *testing.T) {
	c := newTestClient(t, nil)
	res, err := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession        bool `json:"loadSession"`
			PromptCapabilities struct {
				Image           *bool `json:"image"`
				Audio           *bool `json:"audio"`
				EmbeddedContext bool  `json:"embeddedContext"`
			} `json:"promptCapabilities"`
		} `json:"agentCapabilities"`
		AuthMethods []any `json:"authMethods"`
	}
	if err := json.Unmarshal(res, &got); err != nil {
		t.Fatal(err)
	}
	pc := got.AgentCapabilities.PromptCapabilities
	// loadSession stays false until session/load lands (W4f-3).
	if got.ProtocolVersion != 1 || got.AgentCapabilities.LoadSession || !pc.EmbeddedContext ||
		pc.Image == nil || *pc.Image || pc.Audio == nil || *pc.Audio {
		t.Fatalf("initialize = %s", res)
	}
	if got.AuthMethods == nil || len(got.AuthMethods) != 0 {
		t.Fatalf("authMethods must be [], got %s", res)
	}

	// A client asking for a newer version gets the agent's latest.
	res, err = c.call("initialize", map[string]any{"protocolVersion": 2, "clientCapabilities": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(res, &got)
	if got.ProtocolVersion != 1 {
		t.Fatalf("protocolVersion = %d, want 1", got.ProtocolVersion)
	}

	if res, err := c.call("authenticate", map[string]any{"methodId": "x"}); err != nil || string(res) != "{}" {
		t.Fatalf("authenticate = %s %v", res, err)
	}
	if _, err := c.call("no/such/method", map[string]any{}); err == nil || err.Code != CodeMethodNotFound {
		t.Fatalf("unknown method error = %v", err)
	}
}

func TestInitializeRejectsBadParams(t *testing.T) {
	c := newTestClient(t, nil)
	if _, err := c.call("initialize", "not an object"); err == nil || err.Code != CodeInvalidParams {
		t.Fatalf("bad initialize params error = %v", err)
	}
}

// The text of a text block is required by the schema, also when empty;
// the permission prompt's toolCall carries no sessionUpdate.
func TestTypesFollowTheSchema(t *testing.T) {
	b, _ := json.Marshal(TextBlock(""))
	if string(b) != `{"type":"text","text":""}` {
		t.Fatalf("empty text block = %s", b)
	}
	b, _ = json.Marshal(SessionNotification{SessionID: "s", Update: AgentMessageChunk("hi")})
	if string(b) != `{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}` {
		t.Fatalf("agent_message_chunk = %s", b)
	}
	b, _ = json.Marshal(RequestPermissionParams{SessionID: "s",
		ToolCall: ToolCallUpdate{ToolCallID: "c1", Title: "bash: ls", Kind: ToolKindExecute, Status: ToolStatusPending},
		Options:  []PermissionOption{{OptionID: "allow_once", Name: "Allow", Kind: OptionAllowOnce}}})
	want := `{"sessionId":"s","toolCall":{"toolCallId":"c1","title":"bash: ls","kind":"execute","status":"pending"},"options":[{"optionId":"allow_once","name":"Allow","kind":"allow_once"}]}`
	if string(b) != want {
		t.Fatalf("request_permission = %s", b)
	}
	b, _ = json.Marshal(NewPlan(nil))
	if string(b) != `{"sessionUpdate":"plan","entries":[]}` {
		t.Fatalf("plan = %s", b)
	}

	var r RequestPermissionResult
	_ = json.Unmarshal([]byte(`{"outcome":{"outcome":"selected","optionId":"allow_always"}}`), &r)
	if r.Outcome.Outcome != OutcomeSelected || r.Outcome.OptionID != "allow_always" {
		t.Fatalf("outcome = %+v", r)
	}

	var p NewSessionParams
	_ = json.Unmarshal([]byte(`{"cwd":"/w","mcpServers":[{"name":"a","command":"c","args":["x"],"env":[{"name":"K","value":"V"}]},{"type":"http","name":"h","url":"https://x","headers":[]}]}`), &p)
	if len(p.McpServers) != 2 || p.McpServers[0].Type != "" || p.McpServers[0].Env[0].Value != "V" || p.McpServers[1].Type != "http" {
		t.Fatalf("mcpServers = %+v", p.McpServers)
	}

	var pp PromptParams
	_ = json.Unmarshal([]byte(`{"sessionId":"s","prompt":[{"type":"text","text":"hi"},{"type":"resource","resource":{"uri":"file:///a.go","text":"package a"}},{"type":"resource_link","uri":"file:///b.go","name":"b.go"}]}`), &pp)
	if len(pp.Prompt) != 3 || pp.Prompt[1].Resource == nil || pp.Prompt[1].Resource.Text != "package a" || pp.Prompt[2].URI != "file:///b.go" {
		t.Fatalf("prompt = %+v", pp.Prompt)
	}
}
