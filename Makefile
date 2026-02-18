.PHONY: build install clean help test dev verify import-key

# Default target
help:
	@echo "Celeste CLI Build Commands"
	@echo "=========================="
	@echo "  make build        - Build celeste binary in current directory"
	@echo "  make install      - Build and install to ~/.local/bin/celeste"
	@echo "  make dev          - Build, install, and test in PATH"
	@echo "  make clean        - Remove local binary"
	@echo "  make test         - Run installed binary test"
	@echo "  make help         - Show this help message"
	@echo ""
	@echo "Security Commands"
	@echo "================="
	@echo "  make verify FILE=<file>  - Verify downloaded release (requires FILE=)"
	@echo "  make import-key          - Import GPG signing key from Keybase"

# Build the binary
build:
	@echo "🔨 Building Celeste..."
	@cd cmd/celeste && go build -o ../../celeste .
	@echo "✅ Build complete: ./celeste"

# Build and install to PATH
install: build
	@echo "📦 Installing to PATH..."
	@cp celeste ~/.local/bin/celeste
	@chmod +x ~/.local/bin/celeste
	@echo "✅ celeste installed to ~/.local/bin/celeste"

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
