package builtin

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// RegisterAll registers all built-in tools with the registry.
// tracker and snapshots are optional; pass nil to disable file checkpointing.
func RegisterAll(registry *tools.Registry, workspace string, configLoader ConfigLoader, tracker *checkpoints.FileTracker, snapshots *checkpoints.SnapshotManager) {
	// Dev tools — available in Agent, Claw, Chat
	if workspace != "" {
		var readOpts []ReadFileOption
		var writeOpts []WriteFileOption
		var patchOpts []PatchFileOption

		if tracker != nil {
			readOpts = append(readOpts, WithReadFileTracker(tracker))
			writeOpts = append(writeOpts, WithWriteFileTracker(tracker))
			patchOpts = append(patchOpts, WithPatchFileTracker(tracker))
		}
		if snapshots != nil {
			writeOpts = append(writeOpts, WithWriteFileSnapshots(snapshots))
			patchOpts = append(patchOpts, WithPatchFileSnapshots(snapshots))
		}

		registry.RegisterWithModes(NewBashTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewReadFileTool(workspace, readOpts...), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewWriteFileTool(workspace, writeOpts...), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewPatchFileTool(workspace, patchOpts...), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewListFilesTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewSearchTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)

		// Git tools — available in all modes (read-only, always useful)
		registry.RegisterWithModes(NewGitStatusTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewGitLogTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
	}

	// Skill tools that require config — Chat and Claw only
	if configLoader != nil {
		registry.RegisterWithModes(NewWeatherTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewTarotTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewTwitchTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewYouTubeTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewUpscaleImageTool(configLoader), tools.ModeChat, tools.ModeClaw)
		RegisterCryptoTools(registry, configLoader)
	}

	// Web tools — available in Agent, Claw, Chat
	registry.RegisterWithModes(NewWebSearchTool(), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
	registry.RegisterWithModes(NewWebFetchTool(), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)

	// Config-free skill tools — Chat and Claw only
	registry.RegisterWithModes(NewCurrencyTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewBase64EncodeTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewBase64DecodeTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewHashTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewUUIDTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewPasswordTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewReminderSetTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewReminderListTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewNoteSaveTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewNoteGetTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewNoteListTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewQRCodeTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewUnitConverterTool(), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewTimezoneConverterTool(), tools.ModeChat, tools.ModeClaw)

	// Task tracking — available in Agent and Claw modes
	registry.RegisterWithModes(NewTodoTool(), tools.ModeAgent, tools.ModeClaw)
}

// RegisterReadOnlyDevTools registers only read-only dev tools (for restricted agent mode).
func RegisterReadOnlyDevTools(registry *tools.Registry, workspace string) {
	registry.RegisterWithModes(NewReadFileTool(workspace), tools.ModeAgent)
	registry.RegisterWithModes(NewListFilesTool(workspace), tools.ModeAgent)
	registry.RegisterWithModes(NewSearchTool(workspace), tools.ModeAgent)
}

// RegisterCodeGraphTools registers code graph tools with the given indexer.
// Called after the indexer is initialized during startup.
func RegisterCodeGraphTools(registry *tools.Registry, indexer *codegraph.Indexer) {
	registry.RegisterWithModes(NewCodeSearchTool(indexer), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
	registry.RegisterWithModes(NewCodeGraphTool(indexer), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
	registry.RegisterWithModes(NewCodeSymbolsTool(indexer), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
}

// RegisterCryptoTools registers all crypto/blockchain tools.
func RegisterCryptoTools(registry *tools.Registry, configLoader ConfigLoader) {
	registry.RegisterWithModes(NewIPFSTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewAlchemyTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewBlockmonTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewWalletSecurityTool(configLoader), tools.ModeChat, tools.ModeClaw)
}
