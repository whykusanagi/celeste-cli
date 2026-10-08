package imagefit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
)

// B64Len is the base64 length of n bytes (standard padding).
func B64Len(n int) int { return 4 * ((n + 2) / 3) }

// Result is the image to send. Changed is false when Data is the input.
type Result struct {
	Data          []byte
	Format        string // what the bytes are: png, jpeg, gif or webp
	Changed       bool
	Width, Height int
	Note          string // e.g. "first frame of an animated GIF"
}

// FitError says which limit an image can't meet; its text is shown to the
// model (read_file) or sent in the image's place (backends).
type FitError struct {
	Limits Limits
	B64    int
	Format string
	Reason string
}

// unreadable starts the Reason of an image that would not decode.
const unreadable = "not a readable image: "

func (e *FitError) Error() string {
	if detail, ok := strings.CutPrefix(e.Reason, unreadable); ok {
		// No size limit was broken; don't lead with one.
		return fmt.Sprintf("%s image is not readable: %s", e.Format, detail)
	}
	return fmt.Sprintf("%s allows %s per image (base64) in %s; this %s image is %s (%s)",
		e.Limits.Provider, limitMB(e.Limits.MaxB64), strings.Join(e.Limits.Formats, "/"), e.Format, mb(e.B64), e.Reason)
}

func mb(n int) string { return fmt.Sprintf("%.1f MB", float64(n)/mib) }

// limitMB writes a whole number of MiB without decimals ("5 MB").
func limitMB(n int) string {
	if n%mib == 0 {
		return fmt.Sprintf("%d MB", n/mib)
	}
	return mb(n)
}

const (
	maxAttempts = 8
	minEdge     = 64
	// maxDecodePixels bounds the memory a decode can take (a small file
	// can claim huge dimensions): 24 MP, about 96 MB as RGBA plus the
	// decoder's own copy. A 16-bit image decodes at twice the bytes per
	// pixel, so it gets half (maxDecodePixels16).
	maxDecodePixels   = 24_000_000
	maxDecodePixels16 = 12_000_000
)

// decodeSlots serializes the decode-and-resize step: read_file runs up to
// 8 calls at once, and each decode can take a few hundred MB.
var decodeSlots = make(chan struct{}, 1)

// decodeHook runs inside a decode slot and returns its release (a test
// seam).
var decodeHook = func() func() { return func() {} }

// wide reports a color model decoded at 16 bits per channel.
func wide(m color.Model) bool {
	return m == color.RGBA64Model || m == color.NRGBA64Model || m == color.Gray16Model
}

// isWebP reports a RIFF/WEBP header, whatever the file was called.
func isWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// Fit returns data unchanged when it fits lim, else a smaller or converted
// copy, else a *FitError (ruling 4). format is the caller's label (the file
// extension); for anything but WebP the bytes decide the result's Format.
func Fit(data []byte, format string, lim Limits) (Result, error) {
	format = normal(strings.ToLower(format))
	if isWebP(data) {
		format = "webp"
	}
	cfg, detected, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if format == "webp" { // ruling 3: never decoded, only its header read
			w, h, sized := webpSize(data)
			if lim.Accepts("webp") && B64Len(len(data)) <= lim.MaxB64 && (!sized || within(w, h, lim.MaxDim)) {
				return Result{Data: data, Format: format, Width: w, Height: h}, nil
			}
			reason := "WebP can't be converted here; convert it to PNG or JPEG"
			if sized && !within(w, h, lim.MaxDim) {
				reason = fmt.Sprintf("%d×%d is over %d px and WebP can't be resized here; convert it to PNG or JPEG", w, h, lim.MaxDim)
			}
			return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: reason}
		}
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: unreadable + err.Error()}
	}
	format = detected
	if lim.Accepts(format) && B64Len(len(data)) <= lim.MaxB64 && within(cfg.Width, cfg.Height, lim.MaxDim) {
		return Result{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}, nil
	}
	budget := int64(maxDecodePixels)
	if wide(cfg.ColorModel) {
		budget = maxDecodePixels16
	}
	if int64(cfg.Width)*int64(cfg.Height) > budget {
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format,
			Reason: fmt.Sprintf("%d×%d is too many pixels to resize here; scale it down first", cfg.Width, cfg.Height)}
	}
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	defer decodeHook()()
	decoded, note, err := decodeFirst(data, format)
	if err != nil {
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: unreadable + err.Error()}
	}
	img := toRGBA(decoded) // once, so resize and encode take the fast paths
	out := "png"
	if format == "jpeg" && lim.Accepts("jpeg") {
		out = "jpeg"
	}
	if !lim.Accepts(out) {
		out = "jpeg"
	}
	if !lim.Accepts(out) {
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: "no format this provider takes can be written here"}
	}
	w, h := scaled(img.Bounds().Dx(), img.Bounds().Dy(), lim.MaxDim)
	last := len(data)
	for range maxAttempts {
		// Each attempt shrinks the previous one, not the full-size source.
		img = resize(img, w, h)
		enc, err := encode(img, out)
		if err != nil {
			return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: "re-encoding failed: " + err.Error()}
		}
		last = len(enc)
		if B64Len(len(enc)) <= lim.MaxB64 {
			return Result{Data: enc, Format: out, Changed: true, Width: w, Height: h, Note: note}, nil
		}
		if out == "png" && lim.Accepts("jpeg") {
			out = "jpeg" // ruling 4: PNG still too big → JPEG before shrinking
			continue
		}
		nw, nh := max(1, w*3/4), max(1, h*3/4)
		if max(nw, nh) < minEdge {
			break
		}
		w, h = nw, nh
	}
	return Result{}, &FitError{Limits: lim, B64: B64Len(last), Format: format, Reason: fmt.Sprintf("still too large at %d px", max(w, h))}
}

func within(w, h, maxDim int) bool { return maxDim == 0 || max(w, h) <= maxDim }

// scaled keeps the aspect ratio with the long edge at most maxDim.
func scaled(w, h, maxDim int) (int, int) {
	if within(w, h, maxDim) {
		return w, h
	}
	if w >= h {
		return maxDim, max(1, h*maxDim/w)
	}
	return max(1, w*maxDim/h), maxDim
}

// decodeFirst decodes the image, and only the first frame of a GIF: the
// other frames are counted from the block structure, never decoded.
func decodeFirst(data []byte, format string) (image.Image, string, error) {
	if format == "gif" {
		img, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		note := ""
		if gifAnimated(data) {
			note = "first frame of an animated GIF"
		}
		return img, note, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, "", err
}

// gifAnimated reports whether a GIF has more than one image descriptor,
// walking its blocks without decoding them. A malformed file reads as
// not animated.
func gifAnimated(data []byte) bool {
	if len(data) < 13 {
		return false
	}
	i := 13
	if data[10]&0x80 != 0 { // global color table
		i += 3 << (data[10]&7 + 1)
	}
	skipSubBlocks := func() bool {
		for i < len(data) {
			n := int(data[i])
			i++
			if n == 0 {
				return true
			}
			i += n
		}
		return false
	}
	frames := 0
	for i < len(data) {
		switch data[i] {
		case 0x21: // extension: label, then sub-blocks
			i += 2
			if !skipSubBlocks() {
				return false
			}
		case 0x2c: // image descriptor
			frames++
			if frames > 1 {
				return true
			}
			if i+10 > len(data) {
				return false
			}
			flags := data[i+9]
			i += 10
			if flags&0x80 != 0 { // local color table
				i += 3 << (flags&7 + 1)
			}
			i++ // LZW minimum code size
			if !skipSubBlocks() {
				return false
			}
		default: // 0x3b trailer, or garbage
			return false
		}
	}
	return false
}

// webpSize reads a WebP's dimensions from its first chunk header (VP8X,
// VP8 or VP8L); ok is false when the header is not one it knows.
func webpSize(data []byte) (w, h int, ok bool) {
	if !isWebP(data) || len(data) < 25 {
		return 0, 0, false
	}
	le24 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
	switch string(data[12:16]) {
	case "VP8X":
		if len(data) < 30 {
			return 0, 0, false
		}
		return le24(data[24:]) + 1, le24(data[27:]) + 1, true
	case "VP8 ":
		if len(data) < 30 || data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(data[26:])) & 0x3fff, int(binary.LittleEndian.Uint16(data[28:])) & 0x3fff, true
	case "VP8L":
		if data[20] != 0x2f {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(data[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, true
	}
	return 0, 0, false
}

func encode(img *image.RGBA, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	switch {
	case format == "jpeg" && img.Opaque():
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	case format == "jpeg": // flatten transparency onto white
		flat := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		err = jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 85})
	default:
		err = png.Encode(&buf, img)
	}
	return buf.Bytes(), err
}

// toRGBA returns img as an *image.RGBA, converting it once if needed
// (image/draw has fast paths from YCbCr, NRGBA, Gray and Paletted).
func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// resize is an area-average (box) downscale of premultiplied pixels; it
// returns src when the size is unchanged.
func resize(src *image.RGBA, w, h int) *image.RGBA {
	b := src.Bounds()
	if b.Dx() == w && b.Dy() == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		y1 = max(y1, y0+1)
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			x1 = max(x1, x0+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					i := src.PixOffset(sx, sy)
					p := src.Pix[i : i+4 : i+4]
					r, g, bl, a, n = r+uint64(p[0]), g+uint64(p[1]), bl+uint64(p[2]), a+uint64(p[3]), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), uint8(a / n)})
		}
	}
	return dst
}
