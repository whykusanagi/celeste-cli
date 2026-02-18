#!/bin/bash
# Automated verification script for Celeste CLI releases
# Verifies GPG signatures and checksums for downloaded binaries

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
EXPECTED_KEY="940490EF09DA31322BF7FD83875849AB1D541C55"
KEYBASE_URL="https://keybase.io/whykusanagi/pgp_keys.asc"
GITHUB_URL="https://github.com/whykusanagi.gpg"
REPO_URL="https://github.com/whykusanagi/celesteCLI"

# Output functions
print_success() { echo -e "${GREEN}✓${NC} $1"; }
print_error() { echo -e "${RED}✗${NC} $1"; }
print_info() { echo -e "${YELLOW}ℹ${NC} $1"; }
print_step() { echo -e "${BLUE}→${NC} $1"; }

# Usage information
usage() {
    echo "Celeste CLI Release Verification Script"
    echo ""
    echo "Usage: $0 <celeste-archive.tar.gz|.zip>"
    echo ""
    echo "Example:"
    echo "  $0 celeste-linux-amd64.tar.gz"
    echo ""
    echo "This script will:"
    echo "  1. Import the GPG signing key (if needed)"
    echo "  2. Download checksums and signatures"
    echo "  3. Verify GPG signature on checksums"
    echo "  4. Verify file checksum"
    echo ""
    exit 1
}

# Check if file was provided
if [ -z "$1" ]; then
    print_error "No file specified"
    usage
fi

ARCHIVE="$1"

# Check if file exists
if [ ! -f "$ARCHIVE" ]; then
    print_error "File not found: $ARCHIVE"
    usage
fi

echo ""
echo "=========================================="
echo "  Celeste CLI Verification"
echo "=========================================="
echo ""
print_info "Verifying: $ARCHIVE"
echo ""

# Step 1: Check if GPG is installed
print_step "Checking for GPG..."
if ! command -v gpg &> /dev/null; then
    print_error "GPG is not installed"
    echo ""
    echo "Install GPG:"
    echo "  macOS:   brew install gnupg"
    echo "  Ubuntu:  sudo apt-get install gnupg"
    echo "  Windows: https://gnupg.org/download/"
    echo ""
    exit 1
fi
print_success "GPG found: $(gpg --version | head -1)"

# Step 2: Check if key is imported
print_step "Checking for signing key..."
if gpg --list-keys "$EXPECTED_KEY" &> /dev/null; then
    print_success "Signing key already imported"

    # Display key info
    echo ""
    gpg --fingerprint "$EXPECTED_KEY" 2>/dev/null | grep -A 1 "pub\|uid" | head -3
    echo ""
else
    print_info "Signing key not found. Need to import."
    echo ""
    echo "Choose import source:"
    echo "  1) Keybase (recommended - verified identity)"
    echo "  2) GitHub (verified account)"
    echo "  3) Key server (decentralized)"
    echo ""
    read -p "Enter choice [1-3]: " choice

    case $choice in
        1)
            print_step "Importing from Keybase..."
            if curl -s "$KEYBASE_URL" | gpg --import 2>&1; then
                print_success "Key imported from Keybase"
            else
                print_error "Failed to import from Keybase"
                exit 1
            fi
            ;;
        2)
            print_step "Importing from GitHub..."
            if curl -s "$GITHUB_URL" | gpg --import 2>&1; then
                print_success "Key imported from GitHub"
            else
                print_error "Failed to import from GitHub"
                exit 1
            fi
            ;;
        3)
            print_step "Importing from key server..."
            if gpg --keyserver keys.openpgp.org --recv-keys "$EXPECTED_KEY" 2>&1; then
                print_success "Key imported from key server"
            else
                print_error "Failed to import from key server"
                exit 1
            fi
            ;;
        *)
            print_error "Invalid choice"
            exit 1
            ;;
    esac

    echo ""
    print_info "Verify this fingerprint matches:"
    echo "  9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55"
    echo ""
    gpg --fingerprint "$EXPECTED_KEY" 2>/dev/null | grep -A 1 "Key fingerprint"
    echo ""
    read -p "Does the fingerprint match? [y/N]: " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        print_error "Fingerprint verification failed. Aborting."
        exit 1
    fi
    print_success "Fingerprint verified by user"
fi

# Step 3: Determine version from filename
# Extract version from archive name if present
if [[ "$ARCHIVE" =~ celeste-([^-]+)-([^.]+) ]]; then
    PLATFORM="${BASH_REMATCH[1]}"
    ARCH="${BASH_REMATCH[2]}"
    print_info "Detected: $PLATFORM/$ARCH"
fi

# Step 4: Download checksums if not present
RELEASE_URL="$REPO_URL/releases/latest/download"

print_step "Downloading checksums and signatures..."

if [ ! -f "checksums.txt" ]; then
    if curl -sLO "$RELEASE_URL/checksums.txt"; then
        print_success "Downloaded checksums.txt"
    else
        print_error "Failed to download checksums.txt"
        exit 1
    fi
else
    print_info "Using existing checksums.txt"
fi

if [ ! -f "checksums.txt.asc" ]; then
    if curl -sLO "$RELEASE_URL/checksums.txt.asc"; then
        print_success "Downloaded checksums.txt.asc"
    else
        print_error "Failed to download checksums.txt.asc"
        exit 1
    fi
else
    print_info "Using existing checksums.txt.asc"
fi

# Step 5: Verify GPG signature
echo ""
print_step "Verifying GPG signature..."

if gpg --verify checksums.txt.asc checksums.txt 2>&1 | tee /tmp/gpg_output.txt | grep -q "Good signature"; then
    print_success "GPG signature verified"

    # Extract signer info
    SIGNER=$(grep "using RSA key" /tmp/gpg_output.txt | tail -1)
    if [ -n "$SIGNER" ]; then
        echo "  $SIGNER"
    fi
else
    print_error "GPG signature verification FAILED"
    echo ""
    cat /tmp/gpg_output.txt
    echo ""
    print_error "The checksums file signature is invalid!"
    print_error "DO NOT use this download. It may be compromised."
    echo ""
    exit 1
fi

# Clean up temp file
rm -f /tmp/gpg_output.txt

# Step 6: Verify checksum
echo ""
print_step "Verifying file checksum..."

# Detect checksum command
if command -v sha256sum &> /dev/null; then
    CHECKSUM_CMD="sha256sum"
elif command -v shasum &> /dev/null; then
    CHECKSUM_CMD="shasum -a 256"
else
    print_error "No checksum command found (need sha256sum or shasum)"
    exit 1
fi

# Verify checksum
if $CHECKSUM_CMD --check --ignore-missing checksums.txt 2>&1 | grep -q "OK"; then
    print_success "Checksum verified"
    $CHECKSUM_CMD --check --ignore-missing checksums.txt 2>&1 | grep "OK"
else
    print_error "Checksum verification FAILED"
    echo ""
    $CHECKSUM_CMD --check --ignore-missing checksums.txt
    echo ""
    print_error "The file checksum does not match!"
    print_error "DO NOT use this download. It may be corrupted or compromised."
    echo ""
    exit 1
fi

# Success!
echo ""
echo "=========================================="
print_success "All verifications passed!"
echo "=========================================="
echo ""
echo "$ARCHIVE is authentic and safe to use."
echo ""
echo "Next steps:"
echo "  1. Extract: tar xzf $ARCHIVE  (or unzip for .zip)"
echo "  2. Install: sudo mv celeste-* /usr/local/bin/celeste"
echo "  3. Verify:  celeste --version"
echo ""
echo "Documentation:"
echo "  https://github.com/whykusanagi/celesteCLI/blob/main/VERIFICATION.md"
echo ""
