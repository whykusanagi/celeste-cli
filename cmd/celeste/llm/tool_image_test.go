package llm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"strings"
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
		Metadata: map[string]any{"type": "image", "base64": tinyJPEG, "filename": "shot.jpg", "format": "jpeg"}}}
	want := "data:image/jpeg;base64," + tinyJPEG

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

// Anthropic sends a tool result's image as a base64 image block with its
// media type, after the tool result.
func TestToolImageReachesAnthropic(t *testing.T) {
	b, err := NewAnthropicBackend(&Config{APIKey: "k", Model: "claude-x"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []tui.ChatMessage{{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "ok",
		Metadata: map[string]any{"type": "image", "base64": tinyJPEG, "filename": "shot.jpg", "format": "jpeg"}}}
	raw, err := json.Marshal(b.convertMessages(msgs))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"media_type":"image/jpeg"`, `"data":"` + tinyJPEG + `"`, `[Image from tool result: shot.jpg]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
}

// tinyJPEG is a real 2×2 JPEG in base64: send-time fitting (#239) refuses
// bytes that are not an image, so converter tests need a decodable one.
var tinyJPEG = func() string {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}()
