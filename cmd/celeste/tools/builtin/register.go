package builtin

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// RegisterAll registers all built-in tools with the registry.
// tracker and snapshots are optional; pass nil to disable file checkpointing.
// policy is bash's OS sandbox (2.0 W4); nil runs bash with the denylist alone.
func RegisterAll(registry *tools.Registry, workspace string, configLoader ConfigLoader, tracker *checkpoints.FileTracker, snapshots *checkpoints.SnapshotManager, policy *sandbox.Policy) {
	// Dev tools — available in Agent and Chat
	if workspace != "" {
		var readOpts []ReadFileOption
		var writeOpts []WriteFileOption
		var patchOpts []PatchFileOption
		var spliceOpts []SpliceFileOption

		if tracker != nil {
			readOpts = append(readOpts, WithReadFileTracker(tracker))
			writeOpts = append(writeOpts, WithWriteFileTracker(tracker))
			patchOpts = append(patchOpts, WithPatchFileTracker(tracker))
			spliceOpts = append(spliceOpts, WithSpliceFileTracker(tracker))
		}
		if snapshots != nil {
			writeOpts = append(writeOpts, WithWriteFileSnapshots(snapshots))
			patchOpts = append(patchOpts, WithPatchFileSnapshots(snapshots))
			spliceOpts = append(spliceOpts, WithSpliceFileSnapshots(snapshots))
		}

		registry.RegisterWithModes(NewBashTool(workspace, policy), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewReadFileTool(workspace, readOpts...), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewWriteFileTool(workspace, writeOpts...), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewPatchFileTool(workspace, patchOpts...), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewSpliceFileTool(workspace, spliceOpts...), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewListFilesTool(workspace), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewSearchTool(workspace), tools.ModeAgent, tools.ModeChat)

		// Restores tool results that context compaction pruned (#174).
		registry.RegisterWithModes(NewRecallToolResultTool(nil), tools.ModeAgent, tools.ModeChat)

		// Git tools — available in all modes (read-only, always useful)
		registry.RegisterWithModes(NewGitStatusTool(workspace), tools.ModeAgent, tools.ModeChat)
		registry.RegisterWithModes(NewGitLogTool(workspace), tools.ModeAgent, tools.ModeChat)
	}

	// Skill tools that require config — Chat only
	RegisterConfigTools(registry, configLoader)

	// Interactive question tool — only meaningful when a TUI ask bridge exists;
	// degrades to an error result elsewhere.
	registry.RegisterWithModes(NewAskTool(registry), tools.ModeChat, tools.ModeAgent)

	// Dynamic tool discovery — MUST stay visible so the model can pull hidden tools back in.
	registry.RegisterWithModes(NewFindToolsTool(registry), tools.ModeAgent, tools.ModeChat)

	// Web tools — available in Agent and Chat
	registry.RegisterWithModes(NewWebSearchTool(), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewWebFetchTool(), tools.ModeAgent, tools.ModeChat)

	// Config-free skill tools — Chat only
	registry.RegisterWithModes(NewCurrencyTool(), tools.ModeChat)
	registry.RegisterWithModes(NewBase64EncodeTool(), tools.ModeChat)
	registry.RegisterWithModes(NewBase64DecodeTool(), tools.ModeChat)
	registry.RegisterWithModes(NewHashTool(), tools.ModeChat)
	registry.RegisterWithModes(NewUUIDTool(), tools.ModeChat)
	registry.RegisterWithModes(NewPasswordTool(), tools.ModeChat)
	registry.RegisterWithModes(NewReminderSetTool(), tools.ModeChat)
	registry.RegisterWithModes(NewReminderListTool(), tools.ModeChat)
	registry.RegisterWithModes(NewNoteSaveTool(), tools.ModeChat)
	registry.RegisterWithModes(NewNoteGetTool(), tools.ModeChat)
	registry.RegisterWithModes(NewNoteListTool(), tools.ModeChat)
	registry.RegisterWithModes(NewQRCodeTool(), tools.ModeChat)
	registry.RegisterWithModes(NewUnitConverterTool(), tools.ModeChat)
	registry.RegisterWithModes(NewTimezoneConverterTool(), tools.ModeChat)
	registry.RegisterWithModes(NewTTSTool(workspace), tools.ModeChat)
	registry.RegisterWithModes(NewAudioProjectTool(workspace), tools.ModeChat, tools.ModeAgent)

	// Memory tool — available in all modes with workspace
	if workspace != "" {
		registry.RegisterWithModes(NewSaveMemoryTool(workspace), tools.ModeAgent, tools.ModeChat)
	}

	// Task tracking — available in all modes with workspace
	if workspace != "" {
		registry.RegisterWithModes(NewTodoTool(workspace), tools.ModeAgent, tools.ModeChat)
	}
}

// RegisterConfigTools registers the chat skills that need a config loader
// (weather, tarot, twitch, youtube, upscale and the crypto tools), Chat only.
// A nil loader registers nothing. RegisterAll calls it; the chat
// calls it on top of loop.Setup's registry, which has no loader (2.0 F2d).
func RegisterConfigTools(registry *tools.Registry, configLoader ConfigLoader) {
	if configLoader == nil {
		return
	}
	registry.RegisterWithModes(NewWeatherTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewTarotTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewTwitchTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewYouTubeTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewUpscaleImageTool(configLoader), tools.ModeChat)
	RegisterCryptoTools(registry, configLoader)
}

// RegisterCodeGraphTools registers code graph tools with the given indexer.
// Called after the indexer is initialized during startup.
func RegisterCodeGraphTools(registry *tools.Registry, indexer *codegraph.Indexer) {
	registry.RegisterWithModes(NewCodeSearchTool(indexer), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewCodeGraphTool(indexer), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewCodeSymbolsTool(indexer), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewCodeReviewTool(indexer), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewCodeImpactTool(indexer), tools.ModeAgent, tools.ModeChat)
	registry.RegisterWithModes(NewCodeSnapshotTool(indexer), tools.ModeAgent, tools.ModeChat)
}

// RegisterCollectionsTools registers collections search if active collections exist.
func RegisterCollectionsTools(registry *tools.Registry, cfg *config.Config) {
	if cfg.Collections != nil && len(cfg.Collections.ActiveCollections) > 0 && cfg.APIKey != "" {
		registry.RegisterWithModes(NewCollectionsSearchTool(cfg), tools.ModeAgent, tools.ModeChat)
	}
}

// RegisterCryptoTools registers all crypto/blockchain tools.
func RegisterCryptoTools(registry *tools.Registry, configLoader ConfigLoader) {
	registry.RegisterWithModes(NewIPFSTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewAlchemyTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewBlockmonTool(configLoader), tools.ModeChat)
	registry.RegisterWithModes(NewWalletSecurityTool(configLoader), tools.ModeChat)
}
