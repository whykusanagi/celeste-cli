// Package imagefittest builds test images: noise that PNG can't compress,
// so a small size in pixels is still large in bytes.
package imagefittest

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"math/rand/v2"
	"testing"
)

// NoisyPNG is a w×h RGBA PNG of seeded noise (about 4 bytes per pixel).
func NoisyPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(r.Uint32())
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// AnimatedGIF is a two-frame 32×32 GIF; frame 0 is red, frame 1 blue.
func AnimatedGIF(t testing.TB) []byte {
	t.Helper()
	frame := func(c color.Color) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 32, 32), palette.Plan9)
		for i := range p.Pix {
			p.Pix[i] = uint8(p.Palette.Index(c))
		}
		return p
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{frame(color.RGBA{255, 0, 0, 255}), frame(color.RGBA{0, 0, 255, 255})},
		Delay: []int{10, 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// WebP is a minimal RIFF/WEBP header followed by filler. imagefit never
// decodes WebP, so only the header and the length matter.
func WebP() []byte { return append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...) }
