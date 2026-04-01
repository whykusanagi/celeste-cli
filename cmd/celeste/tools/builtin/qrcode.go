package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// QRCodeTool generates QR codes from text.
type QRCodeTool struct {
	BaseTool
}

// NewQRCodeTool creates a QRCodeTool.
func NewQRCodeTool() *QRCodeTool {
	return &QRCodeTool{
		BaseTool: BaseTool{
			ToolName:        "generate_qr_code",
			ToolDescription: "Generate a QR code from text or URL",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Text or URL to encode in QR code",
					},
					"size": map[string]any{
						"type":        "integer",
						"description": "QR code size in pixels (default: 256, min: 64, max: 1024)",
					},
				},
				"required": []string{"text"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"text"},
		},
	}
}

func (t *QRCodeTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	text, ok := input["text"].(string)
	if !ok || text == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text or URL you want to encode in the QR code.",
			map[string]any{
				"skill": "generate_qr_code",
				"field": "text",
			},
		))
	}

	size := 256
	if s, ok := input["size"].(float64); ok {
		size = int(s)
		if size < 64 {
			size = 64
		}
		if size > 1024 {
			size = 1024
		}
	}

	qr, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to generate QR code",
			"An internal error occurred while generating the QR code. Please try again.",
			map[string]any{
				"skill": "generate_qr_code",
				"error": err.Error(),
			},
		))
	}

	pngData, err := qr.PNG(size)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to encode QR code as PNG",
			"An internal error occurred while encoding the QR code. Please try again.",
			map[string]any{
				"skill": "generate_qr_code",
				"error": err.Error(),
			},
		))
	}

	homeDir, _ := os.UserHomeDir()
	qrDir := filepath.Join(homeDir, ".celeste", "qr_codes")
	os.MkdirAll(qrDir, 0755)

	filename := fmt.Sprintf("qr_%d.png", time.Now().Unix())
	filePath := filepath.Join(qrDir, filename)

	if err := os.WriteFile(filePath, pngData, 0644); err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to save QR code file",
			"An internal error occurred while saving the QR code. Please try again.",
			map[string]any{
				"skill":    "generate_qr_code",
				"error":    err.Error(),
				"filepath": filePath,
			},
		))
	}

	return resultFromMap(map[string]any{
		"text":     text,
		"size":     size,
		"filepath": filePath,
		"success":  true,
	})
}
