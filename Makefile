.PHONY: build install clean help test dev verify import-key sync-persona sync-theme tui-snapshots smoke-cli smoke

# Install destination (override with: make install BIN=/custom/path/celeste)
BIN ?= $(HOME)/.local/bin/celeste

# Stamp the build commit into the binary, the same way release.yml does.
# Without this a locally built binary reports CommitSHA="dev" and the bare
# release-please version, so `celeste version` and `celeste_status` read
# identically whether you are running a shipped release or a local build
# several merges ahead — which is exactly how a stale MCP server hides.
# -dirty marks an uncommitted tree so a hand-patched build is never mistaken
# for the commit it was based on.
#
# The value is sanitised to [A-Za-z0-9._-] because it is interpolated into a
# shell recipe: git accepts tag names containing shell metacharacters, so an
# untrusted tag like `v1.0$(...)-tag` would otherwise execute during `make build`
# in a clone of someone else's history.
COMMIT ?= $(shell (git describe --always --dirty --abbrev=7 2>/dev/null || echo dev) | sed 's/[^A-Za-z0-9._-]/_/g')

# GO_LDFLAGS is private and APPENDS to any caller-supplied LDFLAGS rather than
# yielding to it. It was `LDFLAGS ?=`, which meant a pre-set LDFLAGS — an env var
# nix shells, distro packaging and CI images routinely export — silently dropped
# the stamp and put the binary back to CommitSHA="dev", precisely in the
# environments where nobody would notice. Callers can still pass their own flags.
GO_LDFLAGS := -X main.CommitSHA=$(COMMIT) $(LDFLAGS)

# Default target
help:
	@echo "Celeste CLI Build Commands"
	@echo "=========================="
	@echo "  make build        - Build celeste binary in current directory"
	@echo "  make install      - Build and install to ~/.local/bin/celeste"
	@echo "  make dev          - Build, install, and test in PATH"
	@echo "  make clean        - Remove local binary"
	@echo "  make test         - Run installed binary test"
	@echo "  make tui-snapshots - Render every TUI component to PNGs (test-output/tui/)"
	@echo "  make smoke-cli    - Drive the binary through new-feature paths (live model)"
	@echo "  make smoke        - Build + TUI snapshots + CLI smoke (release gate)"
	@echo "  make help         - Show this help message"
	@echo ""
	@echo "Security Commands"
	@echo "================="
	@echo "  make verify FILE=<file>  - Verify downloaded release (requires FILE=)"
	@echo "  make import-key          - Import GPG signing key from Keybase"

# Build the binary
build:
	@echo "🔨 Building Celeste..."
	@go build -ldflags "$(GO_LDFLAGS)" -o ./celeste ./cmd/celeste
	@echo "✅ Build complete: ./celeste"

# Build and install to PATH.
# Builds straight to the destination (go writes via temp+rename → fresh inode)
# rather than `cp`-ing over the existing binary. On macOS, copying over an
# existing binary invalidates its ad-hoc code signature, so the kernel (AMFI)
# SIGKILLs it at launch ("zsh: killed celeste") even though `codesign -v` still
# reports valid-on-disk. We re-sign explicitly on Darwin to be safe.
install:
	@echo "📦 Installing to $(BIN)..."
	@mkdir -p "$(dir $(BIN))"
	@go build -ldflags "$(GO_LDFLAGS)" -o "$(BIN)" ./cmd/celeste
	@chmod +x "$(BIN)"
	@if [ "$$(uname)" = "Darwin" ]; then \
		codesign --force --sign - "$(BIN)" && echo "🔏 ad-hoc signed (macOS AMFI)"; \
	fi
	@echo "✅ celeste installed to $(BIN)"

# Development workflow: build, install, and test
dev: install
	@echo "🎯 Testing installed binary..."
	@celeste --version
	@echo "✨ Ready for development!"

# Clean up local binary
clean:
	@echo "🧹 Cleaning up..."
	@rm -f celeste
	@echo "✅ Cleanup complete"

# Test the installed binary
test:
	@echo "🧪 Testing celeste binary..."
	@which celeste > /dev/null && echo "✅ celeste found in PATH" || echo "❌ celeste not found in PATH"
	@celeste --version 2>/dev/null && echo "✅ Version check passed" || echo "⚠️  Version check failed"

# Render every sprint TUI component to a PNG for visual release verification.
# Output (test-output/tui/*.png) is gitignored. Requires charmbracelet/freeze.
tui-snapshots:
	@bash scripts/tui-snapshots.sh

# Drive the real binary through new-feature code paths, incl. one live model
# call (sakana/fugu by default). SMOKE_NO_MODEL=1 skips the model call.
smoke-cli:
	@CELESTE="$${CELESTE:-./celeste}" bash scripts/smoke-cli.sh

# Release gate: build, render TUI snapshots, and run the CLI smoke test.
smoke: build tui-snapshots smoke-cli
	@echo "✅ smoke: TUI snapshots rendered + CLI paths verified"

# Verify a downloaded release
verify:
	@if [ -z "$(FILE)" ]; then \
		echo "❌ Error: FILE parameter required"; \
		echo "Usage: make verify FILE=celeste-linux-amd64.tar.gz"; \
		exit 1; \
	fi
	@echo "🔒 Verifying $(FILE)..."
	@chmod +x scripts/verify.sh
	@./scripts/verify.sh $(FILE)

# Sync persona files from celeste-core-persona repo
sync-persona:
	@echo "🔄 Syncing persona files from celeste-core-persona..."
	@cp ../celeste-core-persona/cli-prompts/celeste_core.json cmd/celeste/prompts/celeste_essence.json
	@cp ../celeste-core-persona/docs/slider-agent-handoff.md docs/slider-agent-handoff.md
	@echo "✅ Persona synced. Run 'go build' and smoke-test prompt load."

# Sync the canonical corrupted-theme color palette into the embedded copy.
# streaming.go consumes cmd/celeste/tui/theme/colors.json via //go:embed, so the
# corruption colors track the theme repo instead of drifting (task 7aa133c9).
sync-theme:
	@echo "🎨 Syncing color palette from corrupted-theme..."
	@cp ../corrupted-theme/src/data/colors.json cmd/celeste/tui/theme/colors.json
	@echo "✅ Theme colors synced. Run 'go build' and 'go test ./cmd/celeste/tui/theme/'."

# Import GPG signing key from Keybase
import-key:
	@echo "🔑 Importing GPG signing key from Keybase..."
	@if ! command -v gpg &> /dev/null; then \
		echo "❌ GPG not found. Install with: brew install gnupg"; \
		exit 1; \
	fi
	@curl -s https://keybase.io/whykusanagi/pgp_keys.asc | gpg --import
	@echo ""
	@echo "✅ Key imported successfully"
	@echo ""
	@echo "Verify fingerprint matches:"
	@echo "  9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55"
	@echo ""
	@gpg --fingerprint 940490EF09DA31322BF7FD83875849AB1D541C55
