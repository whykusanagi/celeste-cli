// Package imagefit makes an image fit a provider's limits (#239): it keeps
// an image that fits, else downscales and re-encodes it with the standard
// library, else says which limit it can't meet.
package imagefit

import "slices"

// Limits is what one provider accepts for a single image. MaxB64 counts the
// base64 string; MaxDim is the long edge in pixels, 0 for no client-side cap.
type Limits struct {
	Provider string
	MaxB64   int
	MaxDim   int
	Formats  []string
}

const mib = 1 << 20

// The values were checked against each provider's vision docs in 2026-10.
var (
	// Universal is the strictest supported provider: what read_file stores.
	Universal = Limits{Provider: "every provider", MaxB64: 5 * mib, MaxDim: 8000, Formats: []string{"png", "jpeg", "gif", "webp"}}
	// Anthropic allows 10 MB base64 on its own API but 5 MB on Bedrock and
	// Google Cloud; 5 MiB is safe on all three.
	Anthropic = Limits{Provider: "Anthropic", MaxB64: 5 * mib, MaxDim: 8000, Formats: []string{"png", "jpeg", "gif", "webp"}}
	// OpenAI documents no per-image byte cap (512 MB per request) and takes
	// only non-animated GIF, so a GIF is always sent as its first frame.
	OpenAI = Limits{Provider: "OpenAI", MaxB64: 20 * mib, Formats: []string{"png", "jpeg", "webp"}}
	// Gemini caps the whole inline request at 20 MB; 18 MiB leaves room for
	// the prompt.
	Gemini = Limits{Provider: "Gemini", MaxB64: 18 * mib, Formats: []string{"png", "jpeg", "webp"}}
	XAI    = Limits{Provider: "xAI", MaxB64: 20 * mib, Formats: []string{"png", "jpeg"}}
)

// ForProvider maps providers.DetectProvider's names onto limits; anything
// unknown gets Universal (ruling 6).
func ForProvider(name string) Limits {
	switch name {
	case "anthropic":
		return Anthropic
	case "openai":
		return OpenAI
	case "gemini", "vertex":
		return Gemini
	case "grok":
		return XAI
	}
	return Universal
}

// Accepts reports whether the provider takes this format ("jpg" is "jpeg").
func (l Limits) Accepts(format string) bool { return slices.Contains(l.Formats, normal(format)) }

// WithMaxDim returns l with a tighter long-edge cap (Anthropic's >20-image rule).
func (l Limits) WithMaxDim(px int) Limits {
	if l.MaxDim == 0 || px < l.MaxDim {
		l.MaxDim = px
	}
	return l
}

func normal(format string) string {
	if format == "jpg" {
		return "jpeg"
	}
	return format
}
