package imagefit

import (
	"encoding/binary"
	"hash/crc32"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngHeaderDepth is pngHeader with a bit depth and color type.
func pngHeaderDepth(w, h uint32, depth, colorType byte) []byte {
	ihdr := []byte("IHDR")
	ihdr = binary.BigEndian.AppendUint32(ihdr, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, depth, colorType, 0, 0, 0)
	data := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d")
	data = append(data, ihdr...)
	return binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(ihdr))
}

// Aikido 806869432: the decoded-pixel budget is small enough that a few
// images resized at once cannot exhaust memory, and smaller again for
// 16-bit images, which decode at twice the bytes per pixel.
func TestFitDecodeBudget(t *testing.T) {
	lim := Anthropic.WithMaxDim(2000)
	_, err := Fit(pngHeaderDepth(6000, 5000, 8, 6), "png", lim) // 30 MP, 8-bit RGBA
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many pixels")

	_, err = Fit(pngHeaderDepth(4000, 4000, 16, 6), "png", lim) // 16 MP, 16-bit RGBA
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many pixels")
}

// Decodes run one at a time, however many tools call Fit together.
func TestFitDecodesOneAtATime(t *testing.T) {
	var cur, peak atomic.Int32
	orig := decodeHook
	decodeHook = func() func() {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		return func() { cur.Add(-1) }
	}
	t.Cleanup(func() { decodeHook = orig })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = Fit(pngHeaderDepth(3000, 3000, 8, 6), "png", Anthropic.WithMaxDim(1000))
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), peak.Load(), "decodes overlapped")
}
