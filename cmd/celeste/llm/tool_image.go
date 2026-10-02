package llm

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/imagefit"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

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

// fitToolImage returns the tool result's image fitted to lim (#239). ok is
// false when md carries no image. A non-empty note means the image can't be
// sent to this provider: send the note as text in its place, never the
// image (img then carries only its Name).
func fitToolImage(md map[string]any, lim imagefit.Limits) (img toolImage, note string, ok bool) {
	img, ok = toolImageOf(md)
	if !ok {
		return toolImage{}, "", false
	}
	fit, note := fitCached(img, lim)
	if note != "" {
		return toolImage{Name: img.Name}, note, true
	}
	return fit, "", true
}

// fitResult is one fitted image: the image to send, or why it can't be.
type fitResult struct {
	img    toolImage
	reason string
}

// fitCache keeps recent re-fits so a history image that needs one is not
// decoded and re-encoded on every turn. Images that fit as they are skip it.
var fitCache = struct {
	sync.Mutex
	m     map[[sha256.Size]byte]fitResult
	order [][sha256.Size]byte
	bytes int
}{m: map[[sha256.Size]byte]fitResult{}}

// The cache holds at most this much base64 and this many entries; the
// oldest go first.
const (
	fitCacheBytes   = 64 << 20
	fitCacheEntries = 256
)

func fitCached(img toolImage, lim imagefit.Limits) (toolImage, string) {
	raw, err := base64.StdEncoding.DecodeString(img.B64)
	if err != nil {
		return toolImage{}, fmt.Sprintf("[image %s omitted: its data is not valid base64]", img.Name)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%s\x00%s\x00", lim.Provider, lim.MaxB64, lim.MaxDim, strings.Join(lim.Formats, ","), img.Format)
	_, _ = io.WriteString(h, img.B64)
	var key [sha256.Size]byte
	h.Sum(key[:0])
	fitCache.Lock()
	r, hit := fitCache.m[key]
	fitCache.Unlock()
	if !hit {
		res, err := imagefit.Fit(raw, img.Format, lim)
		switch {
		case err != nil:
			r = fitResult{reason: err.Error()}
		case !res.Changed:
			img.Format = res.Format // name what the bytes are; the bytes stay as sent
			return img, ""
		default:
			r = fitResult{img: toolImage{Format: res.Format, B64: base64.StdEncoding.EncodeToString(res.Data)}}
		}
		fitCache.Lock()
		if _, ok := fitCache.m[key]; !ok && len(r.img.B64) <= fitCacheBytes {
			for len(fitCache.order) > 0 && (fitCache.bytes+len(r.img.B64) > fitCacheBytes || len(fitCache.order) >= fitCacheEntries) {
				old := fitCache.order[0]
				fitCache.bytes -= len(fitCache.m[old].img.B64)
				delete(fitCache.m, old)
				fitCache.order = fitCache.order[1:]
			}
			fitCache.order = append(fitCache.order, key)
			fitCache.m[key] = r
			fitCache.bytes += len(r.img.B64)
		}
		fitCache.Unlock()
	}
	if r.reason != "" {
		return toolImage{}, fmt.Sprintf("[image %s omitted: %s]", img.Name, r.reason)
	}
	r.img.Name = img.Name
	return r.img, ""
}

// countToolImages counts the messages whose metadata carries an image.
func countToolImages(msgs []tui.ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if _, ok := toolImageOf(m.Metadata); ok {
			n++
		}
	}
	return n
}
