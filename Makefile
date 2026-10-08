.PHONY: build install clean help test dev verify import-key sync-persona persona-dev persona-check sync-theme tui-snapshots smoke-cli smoke

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
COMMIT ?= $(shell (git describe --tags --always --dirty --abbrev=7 2>/dev/null || echo dev) | sed 's/[^A-Za-z0-9._-]/_/g')

# GO_LDFLAGS is private and APPENDS to any caller-supplied LDFLAGS rather than
# yielding to it. It was `LDFLAGS ?=`, which meant a pre-set LDFLAGS — an env var
# nix shells, distro packaging and CI images routinely export — silently dropped
# the stamp and put the binary back to CommitSHA="dev", precisely in the
# environments where nobody would notice. Callers can still pass their own flags.
GO_LDFLAGS := -X main.CommitSHA=$(COMMIT) $(LDFLAGS)

# The persona key (W5, rulings 15 and 20). Official releases inject it from
# the CELESTE_PERSONA_KEY secret; locally it lives in PERSONA_KEY_FILE (mode
# 0600). PERSONA_LDFLAG expands, inside a recipe line, to the -X flag when
# that file is readable and to nothing otherwise. It is never a make
# variable's value, and every line that uses it starts with @, so make never
# echoes the key. -trimpath keeps it out of the binary's build info, which
# otherwise records the whole -ldflags string (go version -m).
PERSONA_KEY_FILE ?= $(HOME)/.celeste/persona.key
PERSONA_PKG := github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts
PERSONA_LDFLAG = $$(test -r "$(PERSONA_KEY_FILE)" && printf -- '-X $(PERSONA_PKG).personaKey=%s' "$$(tr -d ' \r\n' < "$(PERSONA_KEY_FILE)")")
PERSONA_SAY = @if test -r "$(PERSONA_KEY_FILE)"; then echo "🔐 persona key found: this build carries the full persona"; else echo "ℹ no persona key: this build runs the public persona"; fi

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
	@echo "  make sync-persona  - Seal the persona profiles at their pins (needs the key and private checkouts)"
	@echo "  make persona-dev   - Decrypt the persona into a temp directory (needs the key)"
	@echo "  make persona-check - Rebuild the persona at its pins and compare (needs the key and private checkouts)"
	@echo "  make help         - Show this help message"
	@echo ""
	@echo "Security Commands"
	@echo "================="
	@echo "  make verify FILE=<file>  - Verify downloaded release (requires FILE=)"
	@echo "  make import-key          - Import the release key from whykusanagi.asc (fingerprint-checked)"

# Build the binary
build:
	@echo "🔨 Building Celeste..."
	$(PERSONA_SAY)
	@go build -trimpath -ldflags "$(GO_LDFLAGS) $(PERSONA_LDFLAG)" -o ./celeste ./cmd/celeste
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
	$(PERSONA_SAY)
	@go build -trimpath -ldflags "$(GO_LDFLAGS) $(PERSONA_LDFLAG)" -o "$(BIN)" ./cmd/celeste
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

# Seal the persona (W5, #173): rebuild celeste-persona-container's CLI
# profiles at the commits pinned in cmd/celeste/prompts/persona/SOURCE.json,
# in a temp directory outside the repo, and write only their ciphertext
# here. Installs lore into ~/.celeste/persona-lore (never the repo). Needs
# both private checkouts (PERSONA_CORE, PERSONA_CONTAINER; default: siblings
# of this repo) and the key file. Move a pin with PERSONA_CORE_COMMIT=<sha>
# or PERSONA_CONTAINER_COMMIT=<sha>.
sync-persona:
	@CELESTE_PERSONA_KEY_FILE="$(PERSONA_KEY_FILE)" python3 scripts/sync_persona.py

# Decrypt the committed persona into a new private temp directory, to read
# or diff it. Never inside the repo; delete the directory when done. To run
# celeste itself on the full persona, use make install (key-aware).
persona-dev:
	@dir=$$(mktemp -d "$${TMPDIR:-/tmp}/celeste-persona.XXXXXX") && \
		CELESTE_PERSONA_KEY_FILE="$(PERSONA_KEY_FILE)" go run ./scripts/personaseal open -out "$$dir" && \
		echo "persona profiles in $$dir (delete it when done)"

# The full persona check, local only (CI has neither the key nor the
# corpus): rebuild at the pins, compare plaintext hashes with SOURCE.json,
# open the committed ciphertext, then run the key-gated tests on the real
# profiles. Run it before merging any change under persona/.
persona-check:
	@CELESTE_PERSONA_KEY_FILE="$(PERSONA_KEY_FILE)" python3 scripts/sync_persona.py --check
	@CELESTE_PERSONA_KEY_FILE="$(PERSONA_KEY_FILE)" go test ./cmd/celeste/prompts -run TestRealPersona -count=1 -v

# Sync the canonical corrupted-theme color palette into the embedded copy.
# streaming.go consumes cmd/celeste/tui/theme/colors.json via //go:embed, so the
# corruption colors track the theme repo instead of drifting (task 7aa133c9).
sync-theme:
	@echo "🎨 Syncing color palette from corrupted-theme..."
	@cp ../corrupted-theme/src/data/colors.json cmd/celeste/tui/theme/colors.json
	@echo "✅ Theme colors synced. Run 'go build' and 'go test ./cmd/celeste/tui/theme/'."

# Import the release signing key from the repository's own whykusanagi.asc.
# Releases are signed by its signing subkey, which the Keybase copy omits.
# The file must hold exactly the release key (primary plus signing subkey)
# before anything is imported.
RELEASE_KEY_FPR := 940490EF09DA31322BF7FD83875849AB1D541C55
RELEASE_SUBKEY_FPR := F4C254F6EE5D7F086C921DEBA6BB54DDC70EE8FB
import-key:
	@echo "🔑 Importing the release signing key from whykusanagi.asc..."
	@if ! command -v gpg >/dev/null 2>&1; then \
		echo "❌ GPG not found. Install with: brew install gnupg"; \
		exit 1; \
	fi
	@info="$$(gpg --batch --with-colons --import-options show-only --import whykusanagi.asc 2>/dev/null)" || { echo "❌ whykusanagi.asc is not a readable key"; exit 1; }; \
	primaries="$$(printf '%s\n' "$$info" | awk -F: '$$1 == "pub" {p = 1; next} p && $$1 == "fpr" {print $$10; p = 0}')"; \
	fprs="$$(printf '%s\n' "$$info" | awk -F: '$$1 == "fpr" {print $$10}')"; \
	if [ "$$primaries" != "$(RELEASE_KEY_FPR)" ]; then \
		echo "❌ whykusanagi.asc holds key(s) $$primaries, want exactly $(RELEASE_KEY_FPR)"; \
		exit 1; \
	fi; \
	if ! printf '%s\n' "$$fprs" | grep -qx "$(RELEASE_SUBKEY_FPR)"; then \
		echo "❌ whykusanagi.asc lacks the release signing subkey $(RELEASE_SUBKEY_FPR)"; \
		exit 1; \
	fi
	@gpg --batch --import whykusanagi.asc
	@echo ""
	@echo "✅ Release key $(RELEASE_KEY_FPR) imported (signing subkey $(RELEASE_SUBKEY_FPR))"
	@gpg --fingerprint $(RELEASE_KEY_FPR)
