package llm

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestToolImageOf(t *testing.T) {
	img, ok := toolImageOf(map[string]any{"type": "image", "base64": "QUJD", "filename": "a.jpg", "format": "jpeg"})
	if !ok || img.DataURL() != "data:image/jpeg;base64,QUJD" || img.Name != "a.jpg" || img.MediaType() != "image/jpeg" {
		t.Errorf("jpeg: %+v %v", img, ok)
	}
	if img, ok := toolImageOf(map[string]any{"type": "image", "base64": "QUJD"}); !ok || img.DataURL() != "data:image/png;base64,QUJD" {
		t.Errorf("format defaults to png: %+v", img)
	}
	for _, md := range []map[string]any{nil, {"type": "text", "base64": "x"}, {"type": "image"}, {"type": "image", "base64": 1}} {
		if _, ok := toolImageOf(md); ok {
			t.Errorf("no image expected in %v", md)
		}
	}
}

// The Chat Completions and xAI converters follow a tool result's image
// with a user message carrying it as a data URL.
func TestToolImageReachesChatConverters(t *testing.T) {
	msgs := []tui.ChatMessage{{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "ok",
		Metadata: map[string]any{"type": "image", "base64": "QUJD", "filename": "shot.jpg", "format": "jpeg"}}}
	const want = "data:image/jpeg;base64,QUJD"

	oa := NewOpenAIBackend(&Config{APIKey: "k", Model: "gpt-4o"}).convertMessages(msgs)
	if len(oa) != 2 || len(oa[1].MultiContent) != 2 || oa[1].MultiContent[1].ImageURL.URL != want ||
		oa[1].MultiContent[0].Text != "[Attached image from tool result: shot.jpg]" {
		t.Errorf("openai: %+v", oa)
	}
	xb, err := NewXAIBackend(&Config{APIKey: "k", Model: "grok"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	xa := xb.convertMessages(msgs)
	if len(xa) != 2 || len(xa[1].MultiContent) != 2 || xa[1].MultiContent[1].ImageURL.URL != want {
		t.Errorf("xai: %+v", xa)
	}
}
