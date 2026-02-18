package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// HandleCollectionsCommand handles the collections command and its subcommands.
// Usage:
//
//	celeste collections               - Show help
//	celeste collections create <name> - Create a collection
//	celeste collections list          - List all collections
//	celeste collections upload <id> <files...> - Upload documents
//	celeste collections delete <id>   - Delete a collection
//	celeste collections enable <id>   - Add to active set
//	celeste collections disable <id>  - Remove from active set
//	celeste collections show <id>     - Show collection details
func HandleCollectionsCommand(cmd *Command, cfg *config.Config) *CommandResult {
	if len(cmd.Args) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      getCollectionsHelp(),
			ShouldRender: true,
		}
	}

	subcommand := cmd.Args[0]
	subArgs := cmd.Args[1:]

	switch subcommand {
	case "create":
		return handleCollectionsCreate(subArgs, cfg)
	case "list":
		return handleCollectionsList(cfg)
	case "upload":
		return handleCollectionsUpload(subArgs, cfg)
	case "delete":
		return handleCollectionsDelete(subArgs, cfg)
	case "enable":
		return handleCollectionsEnable(subArgs, cfg)
	case "disable":
		return handleCollectionsDisable(subArgs, cfg)
	case "show":
		return handleCollectionsShow(subArgs, cfg)
	default:
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Unknown collections subcommand: %s\n\n%s", subcommand, getCollectionsHelp()),
			ShouldRender: true,
		}
	}
}

func getCollectionsHelp() string {
	return `xAI Collections Management

Usage:
  celeste collections <subcommand> [args...]

Subcommands:
  create <name>              Create a new collection
  list                       List all collections
  upload <id> <files...>     Upload documents to a collection
  delete <id>                Delete a collection
  enable <id>                Add collection to active set (for chat)
  disable <id>               Remove collection from active set
  show <id>                  Show collection details

Examples:
  celeste collections create "my-docs" --description "My documentation"
  celeste collections upload col_123 docs/*.md
  celeste collections list
  celeste collections enable col_123

Note: Requires xAI Management API key (set via config or XAI_MANAGEMENT_API_KEY env var)`
}

// Helper to get management API key
func getManagementAPIKey(cfg *config.Config) (string, error) {
	key := cfg.XAIManagementAPIKey
	if key == "" {
		key = os.Getenv("XAI_MANAGEMENT_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("xAI Management API key not configured.\nSet it with: celeste config --set-management-key <key>\nOr: export XAI_MANAGEMENT_API_KEY=<key>")
	}
	return key, nil
}

// Helper to create collections client
func createCollectionsClient(cfg *config.Config) (*collections.Client, error) {
	key, err := getManagementAPIKey(cfg)
	if err != nil {
		return nil, err
	}
	return collections.NewClient(key), nil
}

// Implementations
func handleCollectionsCreate(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections create <name> [--description \"desc\"]",
			ShouldRender: true,
		}
	}

	name := args[0]
	description := ""

	// Parse --description flag if present
	for i := 1; i < len(args); i++ {
		if args[i] == "--description" || args[i] == "-d" {
			if i+1 < len(args) {
				description = args[i+1]
				break
			}
		}
	}

	// Create client
	client, err := createCollectionsClient(cfg)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ %v", err),
			ShouldRender: true,
		}
	}

	// Create collection
	collectionID, err := client.CreateCollection(name, description)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create collection: %v", err),
			ShouldRender: true,
		}
	}

	msg := fmt.Sprintf(`✅ Collection created successfully!

   Collection ID: %s
   Name: %s`, collectionID, name)

	if description != "" {
		msg += fmt.Sprintf("\n   Description: %s", description)
	}

	msg += fmt.Sprintf(`

Next steps:
  1. Upload documents: celeste collections upload %s <files>
  2. Enable for chat: celeste collections enable %s`, collectionID, collectionID)

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}

func handleCollectionsList(cfg *config.Config) *CommandResult {
	// Create client
	client, err := createCollectionsClient(cfg)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ %v", err),
			ShouldRender: true,
		}
	}

	// List collections
	collections, err := client.ListCollections()
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to list collections: %v", err),
			ShouldRender: true,
		}
	}

	if len(collections) == 0 {
		return &CommandResult{
			Success:      true,
			Message:      "No collections found.\n\nCreate one with: celeste collections create <name>",
			ShouldRender: true,
		}
	}

	// Get active collections for marking
	activeIDs := make(map[string]bool)
	if cfg.Collections != nil {
		for _, id := range cfg.Collections.ActiveCollections {
			activeIDs[id] = true
		}
	}

	// Build output
	msg := fmt.Sprintf("Collections (%d):\n\n", len(collections))
	for i, col := range collections {
		marker := " "
		if activeIDs[col.ID] {
			marker = "✓"
		}

		msg += fmt.Sprintf("%s [%d] %s\n", marker, i+1, col.Name)
		msg += fmt.Sprintf("    ID: %s\n", col.ID)
		if col.Description != "" {
			msg += fmt.Sprintf("    Description: %s\n", col.Description)
		}
		if col.DocumentCount > 0 {
			msg += fmt.Sprintf("    Documents: %d\n", col.DocumentCount)
		}
		msg += fmt.Sprintf("    Created: %s\n", col.CreatedAt.Format("2006-01-02 15:04:05"))
		msg += "\n"
	}

	if len(activeIDs) > 0 {
		msg += "✓ = Active (enabled for chat)"
	}

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}

func handleCollectionsUpload(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 2 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections upload <collection-id> <files...> [--recursive]",
			ShouldRender: true,
		}
	}

	collectionID := args[0]
	paths := []string{}
	recursive := false

	// Parse paths and --recursive flag
	for i := 1; i < len(args); i++ {
		if args[i] == "--recursive" || args[i] == "-r" {
			recursive = true
		} else {
			paths = append(paths, args[i])
		}
	}

	if len(paths) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ No files specified",
			ShouldRender: true,
		}
	}

	// Create client
	client, err := createCollectionsClient(cfg)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ %v", err),
			ShouldRender: true,
		}
	}

	// Collect all files to upload
	var filesToUpload []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Printf("⚠️  Skipping %s: %v\n", path, err)
			continue
		}

		if info.IsDir() {
			if !recursive {
				fmt.Printf("⚠️  Skipping directory %s (use --recursive to upload directories)\n", path)
				continue
			}

			// Walk directory
			err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					filesToUpload = append(filesToUpload, p)
				}
				return nil
			})
			if err != nil {
				fmt.Printf("⚠️  Error walking directory %s: %v\n", path, err)
			}
		} else {
			filesToUpload = append(filesToUpload, path)
		}
	}

	if len(filesToUpload) == 0 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ No files to upload",
			ShouldRender: true,
		}
	}

	// Upload files
	msg := fmt.Sprintf("Uploading %d file(s) to collection %s...\n\n", len(filesToUpload), collectionID)

	uploaded := 0
	skipped := 0

	for i, path := range filesToUpload {
		// Validate
		if err := collections.ValidateDocument(path); err != nil {
			msg += fmt.Sprintf("[%d/%d] ⚠️  Skipped %s: %v\n", i+1, len(filesToUpload), filepath.Base(path), err)
			skipped++
			continue
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			msg += fmt.Sprintf("[%d/%d] ⚠️  Failed to read %s: %v\n", i+1, len(filesToUpload), filepath.Base(path), err)
			skipped++
			continue
		}

		// Determine content type
		ext := strings.ToLower(filepath.Ext(path))
		contentType := "text/plain"
		switch ext {
		case ".md":
			contentType = "text/markdown"
		case ".html", ".htm":
			contentType = "text/html"
		case ".pdf":
			contentType = "application/pdf"
		}

		// Upload
		name := filepath.Base(path)
		_, err = client.UploadDocument(collectionID, name, data, contentType)
		if err != nil {
			msg += fmt.Sprintf("[%d/%d] ❌ Failed %s: %v\n", i+1, len(filesToUpload), name, err)
			skipped++
			continue
		}

		msg += fmt.Sprintf("[%d/%d] ✅ Uploaded %s (%d bytes)\n", i+1, len(filesToUpload), name, len(data))
		uploaded++
	}

	// Summary
	msg += fmt.Sprintf("\n📊 Upload complete: %d uploaded, %d skipped\n", uploaded, skipped)

	if uploaded > 0 {
		msg += "\nNext step: Enable collection for chat\n"
		msg += fmt.Sprintf("  celeste collections enable %s\n", collectionID)
	}

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}

func handleCollectionsDelete(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections delete <collection-id> [--force]",
			ShouldRender: true,
		}
	}

	collectionID := args[0]
	force := false

	// Check for --force flag
	for i := 1; i < len(args); i++ {
		if args[i] == "--force" || args[i] == "-f" {
			force = true
			break
		}
	}

	// Confirm unless --force
	if !force {
		fmt.Printf("⚠️  This will permanently delete collection %s and all its documents.\n", collectionID)
		fmt.Print("Continue? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			return &CommandResult{
				Success:      true,
				Message:      "Cancelled.",
				ShouldRender: true,
			}
		}
	}

	// Create client
	client, err := createCollectionsClient(cfg)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ %v", err),
			ShouldRender: true,
		}
	}

	// Delete collection
	if err := client.DeleteCollection(collectionID); err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to delete collection: %v", err),
			ShouldRender: true,
		}
	}

	// Remove from active collections if present
	manager := collections.NewManager(nil, cfg)
	if err := manager.DisableCollection(collectionID); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not disable collection: %v\n", err)
	}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save config: %v\n", err)
	}

	return &CommandResult{
		Success:      true,
		Message:      "✅ Collection deleted successfully.",
		ShouldRender: true,
	}
}

func handleCollectionsEnable(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections enable <collection-id>",
			ShouldRender: true,
		}
	}

	collectionID := args[0]

	// Create manager
	manager := collections.NewManager(nil, cfg)

	// Enable collection
	if err := manager.EnableCollection(collectionID); err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to enable collection: %v", err),
			ShouldRender: true,
		}
	}

	// Save config
	if err := config.Save(cfg); err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to save config: %v", err),
			ShouldRender: true,
		}
	}

	msg := fmt.Sprintf("✅ Collection enabled: %s\n\n", collectionID)
	msg += fmt.Sprintf("Active collections: %d\n", len(cfg.Collections.ActiveCollections))
	for _, id := range cfg.Collections.ActiveCollections {
		msg += fmt.Sprintf("  - %s\n", id)
	}
	msg += "\nThe collections_search tool is now available in chat."

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}

func handleCollectionsDisable(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections disable <collection-id>",
			ShouldRender: true,
		}
	}

	collectionID := args[0]

	// Create manager
	manager := collections.NewManager(nil, cfg)

	// Disable collection
	if err := manager.DisableCollection(collectionID); err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to disable collection: %v", err),
			ShouldRender: true,
		}
	}

	// Save config
	if err := config.Save(cfg); err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to save config: %v", err),
			ShouldRender: true,
		}
	}

	msg := fmt.Sprintf("✅ Collection disabled: %s\n", collectionID)

	if len(cfg.Collections.ActiveCollections) > 0 {
		msg += fmt.Sprintf("\nRemaining active collections: %d\n", len(cfg.Collections.ActiveCollections))
		for _, id := range cfg.Collections.ActiveCollections {
			msg += fmt.Sprintf("  - %s\n", id)
		}
	} else {
		msg += "\nNo active collections. The collections_search tool is disabled."
	}

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}

func handleCollectionsShow(args []string, cfg *config.Config) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "❌ Usage: celeste collections show <collection-id>",
			ShouldRender: true,
		}
	}

	collectionID := args[0]

	// Create client
	client, err := createCollectionsClient(cfg)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ %v", err),
			ShouldRender: true,
		}
	}

	// Fetch collections (API may not have GetCollection endpoint)
	allCollections, err := client.ListCollections()
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to fetch collections: %v", err),
			ShouldRender: true,
		}
	}

	// Find the collection
	var col *collections.Collection
	for i := range allCollections {
		if allCollections[i].ID == collectionID {
			col = &allCollections[i]
			break
		}
	}

	if col == nil {
		return &CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Collection not found: %s", collectionID),
			ShouldRender: true,
		}
	}

	// Check if active
	isActive := false
	if cfg.Collections != nil {
		for _, id := range cfg.Collections.ActiveCollections {
			if id == collectionID {
				isActive = true
				break
			}
		}
	}

	// Build output
	msg := "\n" + strings.Repeat("=", 60) + "\n"
	msg += fmt.Sprintf("Collection: %s\n", col.Name)
	msg += strings.Repeat("=", 60) + "\n"
	msg += fmt.Sprintf("ID:          %s\n", col.ID)
	if col.Description != "" {
		msg += fmt.Sprintf("Description: %s\n", col.Description)
	}
	status := "Inactive"
	if isActive {
		status = "Active ✓"
	}
	msg += fmt.Sprintf("Status:      %s\n", status)
	msg += fmt.Sprintf("Documents:   %d\n", col.DocumentCount)
	msg += fmt.Sprintf("Created:     %s\n", col.CreatedAt.Format("2006-01-02 15:04:05"))
	msg += strings.Repeat("=", 60)

	if !isActive {
		msg += fmt.Sprintf("\n\nTo enable for chat: celeste collections enable %s", collectionID)
	}

	return &CommandResult{
		Success:      true,
		Message:      msg,
		ShouldRender: true,
	}
}
