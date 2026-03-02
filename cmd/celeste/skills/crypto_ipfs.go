// Package skills provides IPFS skill implementation using Kubo RPC client.
package skills

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	ipath "github.com/ipfs/boxo/coreiface/path"
	"github.com/ipfs/boxo/files"
	"github.com/ipfs/go-cid"
	rpc "github.com/ipfs/kubo/client/rpc"
	"github.com/multiformats/go-multiaddr"
)

type ipfsClient interface {
	Add(ctx context.Context, file files.Node) (string, error)
	Get(ctx context.Context, path ipath.Path) (files.Node, error)
	PinAdd(ctx context.Context, path ipath.Path) error
	PinRm(ctx context.Context, path ipath.Path) error
	ListPins(ctx context.Context) ([]string, error)
}

type kuboIPFSClient struct {
	api *rpc.HttpApi
}

func (c *kuboIPFSClient) Add(ctx context.Context, file files.Node) (string, error) {
	resolvedPath, err := c.api.Unixfs().Add(ctx, file)
	if err != nil {
		return "", err
	}
	return resolvedPath.Cid().String(), nil
}

func (c *kuboIPFSClient) Get(ctx context.Context, path ipath.Path) (files.Node, error) {
	return c.api.Unixfs().Get(ctx, path)
}

func (c *kuboIPFSClient) PinAdd(ctx context.Context, path ipath.Path) error {
	return c.api.Pin().Add(ctx, path)
}

func (c *kuboIPFSClient) PinRm(ctx context.Context, path ipath.Path) error {
	return c.api.Pin().Rm(ctx, path)
}

func (c *kuboIPFSClient) ListPins(ctx context.Context) ([]string, error) {
	pins, err := c.api.Pin().Ls(ctx)
	if err != nil {
		return nil, err
	}

	var cidList []string
	for pin := range pins {
		if pin.Err() != nil {
			continue
		}
		cidList = append(cidList, pin.Path().Cid().String())
	}

	return cidList, nil
}

var newIPFSClient = createIPFSClient

// IPFSSkill returns the IPFS skill definition
func IPFSSkill() Skill {
	return Skill{
		Name:        "ipfs",
		Description: "IPFS decentralized storage operations: upload content/files, download by CID, manage pins. Supports string content and binary files. Works with Infura, Pinata, and custom IPFS nodes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"upload", "download", "pin", "unpin", "list_pins"},
					"description": "IPFS operation to perform",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "String content to upload (for upload operation with text/data)",
				},
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to file to upload (for upload operation with binary files)",
				},
				"cid": map[string]interface{}{
					"type":        "string",
					"description": "Content identifier (for download, pin, unpin operations)",
				},
			},
			"required": []string{"operation"},
		},
	}
}

// IPFSHandler handles IPFS skill execution
func IPFSHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Get configuration
	config, err := configLoader.GetIPFSConfig()
	if err != nil {
		return formatErrorResponse(
			"config_error",
			"IPFS configuration is required",
			"Configure IPFS by setting CELESTE_IPFS_API_KEY environment variable or adding to skills.json",
			map[string]interface{}{
				"skill":          "ipfs",
				"config_command": "Set CELESTE_IPFS_API_KEY=<your_key>",
			},
		), nil
	}

	// Get operation
	operation, ok := args["operation"].(string)
	if !ok || operation == "" {
		return formatErrorResponse(
			"validation_error",
			"Operation is required",
			"Specify one of: upload, download, pin, unpin, list_pins",
			map[string]interface{}{
				"skill": "ipfs",
				"field": "operation",
			},
		), nil
	}

	// Create IPFS client
	client, err := newIPFSClient(config)
	if err != nil {
		return formatErrorResponse(
			"connection_error",
			fmt.Sprintf("Failed to connect to IPFS: %v", err),
			"Check your IPFS configuration and network connection",
			map[string]interface{}{
				"skill":    "ipfs",
				"provider": config.Provider,
			},
		), nil
	}

	// Route to appropriate handler
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()

	switch operation {
	case "upload":
		return handleIPFSUpload(ctx, client, args, config)
	case "download":
		return handleIPFSDownload(ctx, client, args)
	case "pin":
		return handleIPFSPin(ctx, client, args)
	case "unpin":
		return handleIPFSUnpin(ctx, client, args)
	case "list_pins":
		return handleIPFSListPins(ctx, client)
	default:
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unknown operation: %s", operation),
			"Valid operations: upload, download, pin, unpin, list_pins",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": operation,
			},
		), nil
	}
}

// createIPFSClient creates an IPFS client with provider-aware endpoint and auth.
func createIPFSClient(config IPFSConfig) (ipfsClient, error) {
	endpoint := resolveIPFSEndpoint(config)

	// Parse multiaddr
	addr, err := multiaddr.NewMultiaddr(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid IPFS endpoint: %w", err)
	}

	// Create HTTP API client
	client, err := rpc.NewApi(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPFS client: %w", err)
	}

	applyIPFSAuthHeaders(config, client.Headers.Add)
	return &kuboIPFSClient{api: client}, nil
}

func resolveIPFSEndpoint(config IPFSConfig) string {
	if config.GatewayURL != "" {
		return config.GatewayURL
	}

	switch config.Provider {
	case "infura":
		return "/dns/ipfs.infura.io/tcp/5001/https"
	case "pinata":
		return "/dns/api.pinata.cloud/tcp/443/https"
	default:
		return "/ip4/127.0.0.1/tcp/5001"
	}
}

func applyIPFSAuthHeaders(config IPFSConfig, addHeader func(key, value string)) {
	// Set authentication for Infura
	if config.Provider == "infura" && config.ProjectID != "" && config.APISecret != "" {
		auth := base64.StdEncoding.EncodeToString(
			[]byte(fmt.Sprintf("%s:%s", config.ProjectID, config.APISecret)),
		)
		addHeader("Authorization", "Basic "+auth)
	}

	// Set API key for Pinata
	if config.Provider == "pinata" && config.APIKey != "" {
		addHeader("pinata_api_key", config.APIKey)
		if config.APISecret != "" {
			addHeader("pinata_secret_api_key", config.APISecret)
		}
	}
}

// handleIPFSUpload uploads content to IPFS
func handleIPFSUpload(ctx context.Context, client ipfsClient, args map[string]interface{}, config IPFSConfig) (interface{}, error) {
	// Check for file_path first, then content
	filePath, hasFile := args["file_path"].(string)
	content, hasContent := args["content"].(string)

	// Validate input
	if !hasFile && (!hasContent || content == "") {
		return formatErrorResponse(
			"validation_error",
			"Either content or file_path is required for upload operation",
			"Provide string content or a file path to upload to IPFS",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "upload",
			},
		), nil
	}

	if hasFile && hasContent && content != "" {
		return formatErrorResponse(
			"validation_error",
			"Provide either content or file_path, not both",
			"Choose one: string content or file path",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "upload",
			},
		), nil
	}

	var fileNode files.Node
	var size int64
	var filename string
	var uploadType string

	if hasFile && filePath != "" {
		// File upload mode
		uploadType = "file"

		// Open file
		file, err := os.Open(filePath)
		if err != nil {
			return formatErrorResponse(
				"file_error",
				fmt.Sprintf("Failed to open file: %v", err),
				"Check that the file exists and is readable",
				map[string]interface{}{
					"skill":     "ipfs",
					"operation": "upload",
					"file_path": filePath,
				},
			), nil
		}
		defer file.Close()

		// Get file info
		stat, err := file.Stat()
		if err != nil {
			return formatErrorResponse(
				"file_error",
				fmt.Sprintf("Failed to get file info: %v", err),
				"",
				map[string]interface{}{
					"skill":     "ipfs",
					"operation": "upload",
					"file_path": filePath,
				},
			), nil
		}

		size = stat.Size()
		filename = filepath.Base(filePath)

		// Create file node
		fileNode = files.NewReaderFile(file)
	} else {
		// String content mode
		uploadType = "content"
		reader := strings.NewReader(content)
		fileNode = files.NewReaderFile(reader)
		size = int64(len(content))
		filename = "content.txt"
	}

	// Upload to IPFS
	cidStr, err := client.Add(ctx, fileNode)
	if err != nil {
		return formatErrorResponse(
			"upload_error",
			fmt.Sprintf("Failed to upload to IPFS: %v", err),
			"Check your IPFS configuration and try again",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "upload",
				"type":      uploadType,
			},
		), nil
	}

	// Build gateway URL
	gatewayURL := ""
	if config.GatewayURL != "" {
		gatewayURL = fmt.Sprintf("%s/ipfs/%s", config.GatewayURL, cidStr)
	} else {
		gatewayURL = fmt.Sprintf("https://ipfs.io/ipfs/%s", cidStr)
	}

	return map[string]interface{}{
		"success":     true,
		"cid":         cidStr,
		"size":        size,
		"filename":    filename,
		"type":        uploadType,
		"gateway_url": gatewayURL,
		"message":     fmt.Sprintf("Successfully uploaded %s to IPFS", uploadType),
	}, nil
}

// handleIPFSDownload downloads content from IPFS by CID
func handleIPFSDownload(ctx context.Context, client ipfsClient, args map[string]interface{}) (interface{}, error) {
	// Get CID
	cidStr, ok := args["cid"].(string)
	if !ok || cidStr == "" {
		return formatErrorResponse(
			"validation_error",
			"CID is required for download operation",
			"Provide a valid IPFS Content Identifier (CID)",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "download",
			},
		), nil
	}

	// Parse CID
	parsedCID, err := cid.Decode(cidStr)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid CID: %v", err),
			"Provide a valid IPFS Content Identifier",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "download",
				"cid":       cidStr,
			},
		), nil
	}

	// Download content
	path := ipath.New("/ipfs/" + parsedCID.String())
	node, err := client.Get(ctx, path)
	if err != nil {
		return formatErrorResponse(
			"download_error",
			fmt.Sprintf("Failed to download from IPFS: %v", err),
			"Check that the CID exists and is accessible",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "download",
				"cid":       cidStr,
			},
		), nil
	}
	defer node.Close()

	// Read content from file node
	fileNode := files.ToFile(node)
	if fileNode == nil {
		return formatErrorResponse(
			"download_error",
			"Content is not a file",
			"The CID may point to a directory",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "download",
				"cid":       cidStr,
			},
		), nil
	}
	content, err := io.ReadAll(fileNode)
	if err != nil {
		return formatErrorResponse(
			"download_error",
			fmt.Sprintf("Failed to read content: %v", err),
			"",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "download",
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"cid":     cidStr,
		"content": string(content),
		"size":    len(content),
		"message": "Content successfully downloaded from IPFS",
	}, nil
}

// handleIPFSPin pins content on IPFS
func handleIPFSPin(ctx context.Context, client ipfsClient, args map[string]interface{}) (interface{}, error) {
	// Get CID
	cidStr, ok := args["cid"].(string)
	if !ok || cidStr == "" {
		return formatErrorResponse(
			"validation_error",
			"CID is required for pin operation",
			"Provide a valid IPFS Content Identifier to pin",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "pin",
			},
		), nil
	}

	// Parse CID
	parsedCID, err := cid.Decode(cidStr)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid CID: %v", err),
			"Provide a valid IPFS Content Identifier",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "pin",
				"cid":       cidStr,
			},
		), nil
	}

	// Pin content
	path := ipath.New("/ipfs/" + parsedCID.String())
	err = client.PinAdd(ctx, path)
	if err != nil {
		return formatErrorResponse(
			"pin_error",
			fmt.Sprintf("Failed to pin content: %v", err),
			"Check that the CID exists and is accessible",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "pin",
				"cid":       cidStr,
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"cid":     cidStr,
		"message": "Content successfully pinned on IPFS",
	}, nil
}

// handleIPFSUnpin unpins content from IPFS
func handleIPFSUnpin(ctx context.Context, client ipfsClient, args map[string]interface{}) (interface{}, error) {
	// Get CID
	cidStr, ok := args["cid"].(string)
	if !ok || cidStr == "" {
		return formatErrorResponse(
			"validation_error",
			"CID is required for unpin operation",
			"Provide a valid IPFS Content Identifier to unpin",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "unpin",
			},
		), nil
	}

	// Parse CID
	parsedCID, err := cid.Decode(cidStr)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid CID: %v", err),
			"Provide a valid IPFS Content Identifier",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "unpin",
				"cid":       cidStr,
			},
		), nil
	}

	// Unpin content
	path := ipath.New("/ipfs/" + parsedCID.String())
	err = client.PinRm(ctx, path)
	if err != nil {
		return formatErrorResponse(
			"unpin_error",
			fmt.Sprintf("Failed to unpin content: %v", err),
			"Check that the content is currently pinned",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "unpin",
				"cid":       cidStr,
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"cid":     cidStr,
		"message": "Content successfully unpinned from IPFS",
	}, nil
}

// handleIPFSListPins lists all pinned content
func handleIPFSListPins(ctx context.Context, client ipfsClient) (interface{}, error) {
	// List pins
	cidList, err := client.ListPins(ctx)
	if err != nil {
		return formatErrorResponse(
			"list_error",
			fmt.Sprintf("Failed to list pins: %v", err),
			"",
			map[string]interface{}{
				"skill":     "ipfs",
				"operation": "list_pins",
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"pins":    cidList,
		"count":   len(cidList),
		"message": fmt.Sprintf("Found %d pinned items", len(cidList)),
	}, nil
}
