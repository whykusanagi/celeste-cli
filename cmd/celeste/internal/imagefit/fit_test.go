package imagefit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color/palette"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/imagefit/imagefittest"
)

func dims(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

func TestB64Len(t *testing.T) {
	assert.Equal(t, 0, B64Len(0))
	assert.Equal(t, 4, B64Len(1))
	assert.Equal(t, 4, B64Len(3))
	assert.Equal(t, 8, B64Len(4))
}

func TestFitPassesThroughUnchanged(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 64, 64)
	res, err := Fit(in, "png", Universal)
	require.NoError(t, err)
	assert.False(t, res.Changed)
	assert.Equal(t, in, res.Data, "an image that fits keeps its bytes")
	assert.Equal(t, "png", res.Format)
}

func TestFitNamesTheFormatTheBytesAre(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 8, 8)
	res, err := Fit(in, "jpg", Universal) // a PNG saved as .jpg
	require.NoError(t, err)
	assert.False(t, res.Changed)
	assert.Equal(t, in, res.Data)
	assert.Equal(t, "png", res.Format, "the media type must match the bytes, not the extension")
}

func TestFitShrinksOversizedPNGUnderTheLimit(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 1400, 1400) // ~7.8 MB raw, over 5 MiB base64
	require.Greater(t, B64Len(len(in)), Anthropic.MaxB64)
	res, err := Fit(in, "png", Anthropic)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.LessOrEqual(t, B64Len(len(res.Data)), Anthropic.MaxB64)
	w, h := dims(t, res.Data)
	assert.Equal(t, res.Width, w)
	assert.Equal(t, res.Height, h)
}

func TestFitCapsTheLongEdge(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 300, 100)
	res, err := Fit(in, "png", Anthropic.WithMaxDim(150))
	require.NoError(t, err)
	w, h := dims(t, res.Data)
	assert.Equal(t, 150, w)
	assert.Equal(t, 50, h)
}

func TestFitConvertsAFormatTheProviderRejects(t *testing.T) {
	in := imagefittest.AnimatedGIF(t)
	res, err := Fit(in, "gif", XAI)
	require.NoError(t, err)
	assert.Equal(t, "png", res.Format)
	assert.Contains(t, res.Note, "first frame")
	_, err = png.Decode(bytes.NewReader(res.Data))
	assert.NoError(t, err)
}

func TestFitAnimatedGIFKeepsFirstFrame(t *testing.T) {
	res, err := Fit(imagefittest.AnimatedGIF(t), "gif", Gemini) // Gemini takes no GIF
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(res.Data))
	require.NoError(t, err)
	r, _, b, _ := img.At(16, 16).RGBA()
	assert.Greater(t, r, b, "frame 0 is red")
}

func TestFitRefusesOversizedWebP(t *testing.T) {
	big := append(imagefittest.WebP(), make([]byte, Anthropic.MaxB64)...)
	_, err := Fit(big, "webp", Anthropic)
	var fe *FitError
	require.True(t, errors.As(err, &fe))
	assert.Contains(t, err.Error(), "5 MB")
	assert.Contains(t, err.Error(), "PNG or JPEG")
}

func TestFitRefusesWebPForXAI(t *testing.T) {
	_, err := Fit(imagefittest.WebP(), "webp", XAI)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xAI")
}

func TestFitErrorNamesTheProviderAndLimit(t *testing.T) {
	err := &FitError{Limits: Anthropic, B64: 7 << 20, Format: "png", Reason: "still too large at 64 px"}
	msg := err.Error()
	for _, want := range []string{"Anthropic", "5 MB", "7.0 MB", "64 px"} {
		assert.True(t, strings.Contains(msg, want), "%q missing %q", msg, want)
	}
}

func TestForProvider(t *testing.T) {
	assert.Equal(t, Anthropic, ForProvider("anthropic"))
	assert.Equal(t, XAI, ForProvider("grok"))
	assert.Equal(t, Gemini, ForProvider("vertex"))
	assert.Equal(t, Universal, ForProvider("local"))
	assert.Equal(t, Universal, ForProvider("openrouter"))
}

func TestFitRefusesAPixelBombWithoutDecodingIt(t *testing.T) {
	// A tiny PNG header claiming 20000×20000 must be refused from its
	// config alone, never decoded into gigabytes.
	ihdr := []byte("IHDR\x00\x00\x4e\x20\x00\x00\x4e\x20\x08\x00\x00\x00\x00")
	var data []byte
	data = append(data, "\x89PNG\r\n\x1a\n\x00\x00\x00\x0d"...)
	data = append(data, ihdr...)
	data = binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(ihdr))
	_, err := Fit(data, "png", Universal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many pixels")
}

func TestFitTreatsWebPBytesAsWebPWhateverTheName(t *testing.T) {
	_, err := Fit(imagefittest.WebP(), "png", XAI)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PNG or JPEG")
}

func TestFitChecksWebPDimensionsFromItsHeader(t *testing.T) {
	small := imagefittest.WebPSized(1500, 900)
	res, err := Fit(small, "webp", Anthropic.WithMaxDim(2000))
	require.NoError(t, err)
	assert.Equal(t, small, res.Data)
	assert.Equal(t, 1500, res.Width)

	_, err = Fit(imagefittest.WebPSized(2400, 900), "webp", Anthropic.WithMaxDim(2000))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2400×900")
	assert.Contains(t, err.Error(), "PNG or JPEG")
}

func TestWebPSizeReadsAllThreeHeaders(t *testing.T) {
	w, h, ok := webpSize(imagefittest.WebPSized(300, 200))
	assert.True(t, ok)
	assert.Equal(t, [2]int{300, 200}, [2]int{w, h})
	// Lossy VP8: frame tag, start code 9d 01 2a, 14-bit width and height.
	lossy := []byte("RIFF\x00\x00\x00\x00WEBPVP8 \x00\x00\x00\x00\x00\x00\x00\x9d\x01\x2a\x2c\x01\xc8\x00")
	w, h, ok = webpSize(lossy)
	assert.True(t, ok)
	assert.Equal(t, [2]int{300, 200}, [2]int{w, h})
	// Lossless VP8L: signature 0x2f, then width-1 and height-1 in 14 bits each.
	v := uint32(299) | uint32(199)<<14
	lossless := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8L\x00\x00\x00\x00\x2f"), byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	w, h, ok = webpSize(lossless)
	assert.True(t, ok)
	assert.Equal(t, [2]int{300, 200}, [2]int{w, h})
	_, _, ok = webpSize(imagefittest.WebP())
	assert.False(t, ok, "a header it can't read leaves the size unknown")
}

func TestFitStillGIFHasNoFirstFrameNote(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 8, 8), palette.Plan9), nil))
	res, err := Fit(buf.Bytes(), "gif", XAI)
	require.NoError(t, err)
	assert.Equal(t, "png", res.Format)
	assert.Empty(t, res.Note)
}

func TestHeaderReadersSurviveTruncation(t *testing.T) {
	for _, data := range [][]byte{imagefittest.AnimatedGIF(t), imagefittest.WebPSized(10, 10)} {
		for n := range len(data) {
			gifAnimated(data[:n])
			webpSize(data[:n])
		}
	}
	assert.True(t, gifAnimated(imagefittest.AnimatedGIF(t)))
}
