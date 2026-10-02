package imagefit

import (
	"bytes"
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

func (e *FitError) Error() string {
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
	// can claim huge dimensions): 8000×8000, about 256 MB as RGBA.
	maxDecodePixels = 8000 * 8000
)

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
		if format == "webp" { // ruling 3: never decoded
			if lim.Accepts("webp") && B64Len(len(data)) <= lim.MaxB64 {
				return Result{Data: data, Format: format}, nil
			}
			return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format,
				Reason: "WebP can't be converted here; convert it to PNG or JPEG"}
		}
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: "not a readable image: " + err.Error()}
	}
	format = detected
	if lim.Accepts(format) && B64Len(len(data)) <= lim.MaxB64 && within(cfg.Width, cfg.Height, lim.MaxDim) {
		return Result{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}, nil
	}
	if cfg.Width*cfg.Height > maxDecodePixels {
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format,
			Reason: fmt.Sprintf("%d×%d is too many pixels to resize here; scale it down first", cfg.Width, cfg.Height)}
	}
	img, note, err := decodeFirst(data, format)
	if err != nil {
		return Result{}, &FitError{Limits: lim, B64: B64Len(len(data)), Format: format, Reason: err.Error()}
	}
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
		enc, err := encode(resize(img, w, h), out)
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

func decodeFirst(data []byte, format string) (image.Image, string, error) {
	if format == "gif" {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		if len(g.Image) == 0 {
			return nil, "", fmt.Errorf("gif has no frames")
		}
		note := ""
		if len(g.Image) > 1 {
			note = "first frame of an animated GIF"
		}
		return g.Image[0], note, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, "", err
}

func encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == "jpeg" {
		flat := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		err = jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&buf, img)
	}
	return buf.Bytes(), err
}

// resize is an area-average (box) downscale; it returns src when the size
// is unchanged.
func resize(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	if b.Dx() == w && b.Dy() == h {
		return src
	}
	nrgba, fast := src.(*image.NRGBA)
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		y1 = max(y1, y0+1)
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			x1 = max(x1, x0+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					var c color.NRGBA
					if fast {
						i := nrgba.PixOffset(sx, sy)
						c = color.NRGBA{nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3]}
					} else {
						c = color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					}
					r, g, bl, a, n = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A), n+1
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{uint8(r / n), uint8(g / n), uint8(bl / n), uint8(a / n)})
		}
	}
	return dst
}
