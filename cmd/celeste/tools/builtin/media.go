package builtin

import (
	"context"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/venice"
)

// UpscaleImageTool upscales images using Venice.ai.
type UpscaleImageTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewUpscaleImageTool creates an UpscaleImageTool.
func NewUpscaleImageTool(configLoader ConfigLoader) *UpscaleImageTool {
	return &UpscaleImageTool{
		BaseTool: BaseTool{
			ToolName:        "upscale_image",
			ToolDescription: "Upscale and enhance an image using Venice.ai. Provide the file path to the image. Returns the path to the upscaled file.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image_path": map[string]any{
						"type":        "string",
						"description": "Path to the image file to upscale (e.g. ~/Pictures/photo.png)",
					},
					"scale": map[string]any{
						"type":        "integer",
						"description": "Upscale factor, e.g. 2 for 2x resolution (default: 2)",
						"default":     2,
					},
					"creativity": map[string]any{
						"type":        "number",
						"description": "Enhancement creativity level from 0.0 to 1.0 (default: 0.5)",
						"default":     0.5,
					},
				},
				"required": []string{"image_path"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"image_path"},
		},
		configLoader: configLoader,
	}
}

func (t *UpscaleImageTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	veniceConfig, err := t.configLoader.GetVeniceConfig()
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("Venice.ai not configured: %w", err)
	}

	imagePath, ok := input["image_path"].(string)
	if !ok || imagePath == "" {
		return tools.ToolResult{}, fmt.Errorf("image_path is required")
	}

	scale := 2
	if s, ok := input["scale"].(float64); ok {
		scale = int(s)
	}

	creativity := 0.5
	if c, ok := input["creativity"].(float64); ok {
		creativity = c
	}

	config := venice.Config{
		APIKey:  veniceConfig.APIKey,
		BaseURL: veniceConfig.BaseURL,
	}

	params := map[string]any{
		"scale":      scale,
		"creativity": creativity,
	}

	response, err := venice.UpscaleImage(config, imagePath, params)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("upscale failed: %w", err)
	}

	if !response.Success {
		return tools.ToolResult{}, fmt.Errorf("upscale failed: %s", response.Error)
	}

	result := map[string]any{
		"message": fmt.Sprintf("Image upscaled %dx successfully", scale),
	}
	if response.Path != "" {
		result["output_path"] = response.Path
		result["message"] = fmt.Sprintf("Image upscaled %dx and saved to %s", scale, response.Path)
	} else if response.URL != "" {
		result["url"] = response.URL
		result["message"] = fmt.Sprintf("Image upscaled %dx, available at: %s", scale, response.URL)
	}

	return resultFromMap(result)
}
