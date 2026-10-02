package llm

import "fmt"

// toolImage is the image a tool result carries in its metadata (type
// "image", base64, format, filename), as read_file and the image tools set
// it.
type toolImage struct {
	Format string // png when the tool did not say
	B64    string
	Name   string
}

// toolImageOf returns a tool result's image, or false when its metadata
// carries none.
func toolImageOf(md map[string]any) (toolImage, bool) {
	if t, _ := md["type"].(string); t != "image" {
		return toolImage{}, false
	}
	b64, ok := md["base64"].(string)
	if !ok {
		return toolImage{}, false
	}
	format, _ := md["format"].(string)
	if format == "" {
		format = "png"
	}
	name, _ := md["filename"].(string)
	return toolImage{Format: format, B64: b64, Name: name}, true
}

// MediaType is image/<format>.
func (i toolImage) MediaType() string { return "image/" + i.Format }

// DataURL is the image as a data: URL, for the OpenAI-style APIs.
func (i toolImage) DataURL() string { return fmt.Sprintf("data:%s;base64,%s", i.MediaType(), i.B64) }
