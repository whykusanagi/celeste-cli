package builtin

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/imagefit"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/imagefit/imagefittest"
)

func readImage(t *testing.T, name string, data []byte) (content string, md map[string]any, isErr bool) {
	t.Helper()
	ws := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, name), data, 0o600))
	res, err := NewReadFileTool(ws).Execute(context.Background(), map[string]any{"path": name}, nil)
	require.NoError(t, err)
	return res.Content, res.Metadata, res.Error
}

func TestReadFileSmallImageUnchanged(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 32, 32)
	content, md, isErr := readImage(t, "s.png", in)
	require.False(t, isErr)
	assert.Equal(t, base64.StdEncoding.EncodeToString(in), md["base64"])
	assert.Equal(t, "png", md["format"])
	assert.NotContains(t, md, "original_bytes")
	assert.Equal(t, "Image file: s.png (png, "+strconv.Itoa(len(in))+" bytes)", content)
}

func TestReadFileShrinksAnImageOverTheLimit(t *testing.T) {
	in := imagefittest.NoisyPNG(t, 1400, 1400)
	content, md, isErr := readImage(t, "big.png", in)
	require.False(t, isErr, content)
	b64 := md["base64"].(string)
	assert.LessOrEqual(t, len(b64), imagefit.Universal.MaxB64)
	assert.Equal(t, len(in), md["original_bytes"])
	assert.Contains(t, content, "reduced from 1400×1400")
	raw, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	res, err := imagefit.Fit(raw, md["format"].(string), imagefit.Universal)
	require.NoError(t, err)
	assert.False(t, res.Changed, "the stored image already fits every provider")
	assert.Equal(t, md["format"], res.Format, "format names what the bytes are")
}

func TestReadFileRefusesAnImageItCannotFit(t *testing.T) {
	big := append(imagefittest.WebP(), make([]byte, imagefit.Universal.MaxB64)...)
	content, md, isErr := readImage(t, "big.webp", big)
	assert.True(t, isErr)
	assert.Nil(t, md, "no base64 is attached to a refusal")
	assert.Contains(t, content, "5 MB")
	assert.Contains(t, content, "PNG or JPEG")
}
