package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/imagefit"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/imagefit/imagefittest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// imageLoop is a user prompt, an assistant read_file call and its result
// carrying data as an image, the shape a resumed session replays.
func imageLoop(format string, data []byte) []tui.ChatMessage {
	return []tui.ChatMessage{
		{Role: "user", Content: "look"},
		{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: `{"path":"x"}`}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "Image file: x",
			Metadata: map[string]any{"type": "image", "format": format, "filename": "x." + format,
				"base64": base64.StdEncoding.EncodeToString(data)}},
	}
}

func pngDims(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

func TestAnthropicRefitsHistoricImage(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "ok"})
	fakeprovider.AnthropicImageLimit(srv, imagefit.Anthropic.MaxB64)
	_, b := newAnthropicTestClient(t, srv, "claude-opus-5-5")
	_, err := b.SendMessageSync(context.Background(), imageLoop("png", imagefittest.NoisyPNG(t, 1400, 1400)), nil)
	require.NoError(t, err, "a pre-#239 image must be re-fitted, not sent as a 400")
	raw := string(srv.Requests()[0].Raw)
	assert.NotContains(t, raw, "omitted")
	assert.Contains(t, raw, `"type":"image"`)
}

func TestAnthropicManyImagesCapsDimensions(t *testing.T) {
	var msgs []tui.ChatMessage
	for range 21 {
		msgs = append(msgs, imageLoop("png", imagefittest.NoisyPNG(t, 2400, 10))...)
	}
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "ok"})
	_, b := newAnthropicTestClient(t, srv, "claude-opus-5-5")
	_, err := b.SendMessageSync(context.Background(), msgs, nil)
	require.NoError(t, err)
	var body struct {
		Messages []struct {
			Content []struct {
				Type   string                `json:"type"`
				Source struct{ Data string } `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(srv.Requests()[0].Raw, &body))
	n := 0
	for _, m := range body.Messages {
		for _, c := range m.Content {
			if c.Type == "image" {
				n++
				raw, err := base64.StdEncoding.DecodeString(c.Source.Data)
				require.NoError(t, err)
				w, _ := pngDims(t, raw)
				assert.LessOrEqual(t, w, 2000)
			}
		}
	}
	assert.Equal(t, 21, n)
}

func TestAnthropicFewImagesKeepTheirSize(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 2400, 10)
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "ok"})
	_, b := newAnthropicTestClient(t, srv, "claude-opus-5-5")
	_, err := b.SendMessageSync(context.Background(), imageLoop("png", in), nil)
	require.NoError(t, err)
	assert.Contains(t, string(srv.Requests()[0].Raw), base64.StdEncoding.EncodeToString(in), "20 or fewer images are sent byte for byte")
}

func TestXAISwapsWebPForNote(t *testing.T) {
	img, note, ok := fitToolImage(imageLoop("webp", imagefittest.WebP())[2].Metadata, imagefit.XAI)
	require.True(t, ok)
	assert.Empty(t, img.B64)
	assert.Equal(t, "x.webp", img.Name)
	assert.True(t, strings.HasPrefix(note, "[image x.webp omitted: xAI allows"), note)
}

func TestFitToolImageNoImage(t *testing.T) {
	_, _, ok := fitToolImage(map[string]any{"type": "text"}, imagefit.Universal)
	assert.False(t, ok)
}

func TestFitToolImageBadBase64IsANote(t *testing.T) {
	_, note, ok := fitToolImage(map[string]any{"type": "image", "base64": "!!", "filename": "a.png"}, imagefit.Universal)
	require.True(t, ok)
	assert.Contains(t, note, "[image a.png omitted")
}

// Every backend's conversion sends the note, not the image, when the image
// can't be fitted to its provider.
func TestBackendsSendTheNoteInPlaceOfTheImage(t *testing.T) {
	msgs := imageLoop("webp", imagefittest.WebP())
	encode := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return string(b)
	}
	t.Run("xai", func(t *testing.T) {
		got := encode((&XAIBackend{config: &Config{}}).convertMessages(msgs))
		assert.Contains(t, got, "omitted: xAI allows")
		assert.NotContains(t, got, "image_url")
	})
	t.Run("google", func(t *testing.T) {
		got := encode((&GoogleBackend{config: &Config{}}).convertMessagesToGenAI(imageLoop("gif", []byte("not a gif"))))
		assert.Contains(t, got, "omitted: gif image is not readable")
		assert.NotContains(t, got, "inlineData")
	})
	t.Run("openai chat on xAI", func(t *testing.T) {
		got := encode((&OpenAIBackend{config: &Config{BaseURL: "https://api.x.ai/v1"}}).convertMessages(msgs))
		assert.Contains(t, got, "omitted: xAI allows")
		assert.NotContains(t, got, "image_url")
	})
	t.Run("responses", func(t *testing.T) {
		items, _ := responsesInput(msgs, testRespKey, imagefit.XAI)
		got := encode(items)
		assert.Contains(t, got, "omitted: xAI allows")
		assert.NotContains(t, got, "input_image")
	})
}

func TestOpenAILimitsFollowTheEndpoint(t *testing.T) {
	assert.Equal(t, imagefit.OpenAI, openAIImageLimits(""), "no base URL is api.openai.com")
	assert.Equal(t, imagefit.XAI, openAIImageLimits("https://api.x.ai/v1"))
	assert.Equal(t, imagefit.Universal, openAIImageLimits("http://localhost:11434/v1"))
}

func TestResponsesImagePartKeepsTheNameOnANote(t *testing.T) {
	part, name, ok := imagePart(imageLoop("webp", imagefittest.WebP())[2].Metadata, imagefit.XAI)
	require.True(t, ok)
	assert.Equal(t, "input_text", part.Type)
	assert.Equal(t, "x.webp", name)
}

// A WebP over Anthropic's many-image cap is swapped for a note, since it
// can't be resized; under 20 images it goes as is.
func TestAnthropicManyImagesRefusesAWideWebP(t *testing.T) {
	var msgs []tui.ChatMessage
	for range 20 {
		msgs = append(msgs, imageLoop("png", imagefittest.NoisyPNG(t, 8, 8))...)
	}
	msgs = append(msgs, imageLoop("webp", imagefittest.WebPSized(2400, 100))...)
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "ok"})
	_, b := newAnthropicTestClient(t, srv, "claude-opus-5-5")
	_, err := b.SendMessageSync(context.Background(), msgs, nil)
	require.NoError(t, err)
	assert.Contains(t, string(srv.Requests()[0].Raw), "[image x.webp omitted: Anthropic allows")
}
