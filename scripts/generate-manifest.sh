#!/bin/bash
# Manifest generation script for Celeste CLI releases
# Generates a structured JSON manifest with build metadata and artifact checksums

set -e

# Default values
VERSION=""
COMMIT=""
TAG=""
GO_VERSION=""
OUTPUT="manifest.json"
DIST_DIR="dist"

# Parse command-line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            VERSION="$2"
            shift 2
            ;;
        --commit)
            COMMIT="$2"
            shift 2
            ;;
        --tag)
            TAG="$2"
            shift 2
            ;;
        --go-version)
            GO_VERSION="$2"
            shift 2
            ;;
        --output)
            OUTPUT="$2"
            shift 2
            ;;
        --dist-dir)
            DIST_DIR="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 --version <version> --commit <commit> --go-version <go-version> [--tag <tag>] [--output <file>] [--dist-dir <dir>]"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$VERSION" ] || [ -z "$COMMIT" ] || [ -z "$GO_VERSION" ]; then
    echo "Error: Missing required parameters"
    echo "Usage: $0 --version <version> --commit <commit> --go-version <go-version> [--tag <tag>] [--output <file>] [--dist-dir <dir>]"
    exit 1
fi

# Use VERSION for TAG if not provided
if [ -z "$TAG" ]; then
    TAG="$VERSION"
fi

# Get release date in ISO 8601 format
RELEASE_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Start building JSON
cat > "$OUTPUT" << EOF
{
  "version": "$VERSION",
  "release_date": "$RELEASE_DATE",
  "commit": "$COMMIT",
  "tag": "$TAG",
  "go_version": "$GO_VERSION",
  "builder": "GitHub Actions",
  "artifacts": [
EOF

# Process artifacts in dist directory
FIRST=true
for file in "$DIST_DIR"/*.tar.gz "$DIST_DIR"/*.zip; do
    [ -f "$file" ] || continue

    # Extract platform and arch from filename
    # Expected format: celeste-{platform}-{arch}.tar.gz
    filename=$(basename "$file")

    # Parse platform and arch
    if [[ $filename =~ celeste-([^-]+)-([^.]+)\.(tar\.gz|zip) ]]; then
        platform="${BASH_REMATCH[1]}"
        arch="${BASH_REMATCH[2]}"
        ext="${BASH_REMATCH[3]}"
    else
        echo "Warning: Skipping file with unexpected format: $filename"
        continue
    fi

    # Get file size
    size=$(stat -f%z "$file" 2>/dev/null || stat -c%s "$file" 2>/dev/null || echo "0")

    # Calculate checksums
    sha256=$(shasum -a 256 "$file" | awk '{print $1}')
    sha512=$(shasum -a 512 "$file" | awk '{print $1}')

    # Build download URL
    download_url="https://github.com/whykusanagi/celesteCLI/releases/download/${TAG}/${filename}"

    # Add comma if not first artifact
    if [ "$FIRST" = false ]; then
        echo "," >> "$OUTPUT"
    fi
    FIRST=false

    # Add artifact entry
    cat >> "$OUTPUT" << ARTIFACT
    {
      "filename": "$filename",
      "platform": "$platform",
      "arch": "$arch",
      "size": $size,
      "sha256": "$sha256",
      "sha512": "$sha512",
      "download_url": "$download_url"
    }
ARTIFACT
done

# Close artifacts array and add verification info
cat >> "$OUTPUT" << 'EOF'
  ],
  "verification": {
    "pgp_key_id": "875849AB1D541C55",
    "pgp_key_fingerprint": "940490EF09DA31322BF7FD83875849AB1D541C55",
    "keybase_profile": "whykusanagi",
    "github_keys_url": "https://github.com/whykusanagi.gpg",
    "verification_guide": "https://github.com/whykusanagi/celesteCLI/blob/main/VERIFICATION.md"
  }
}
EOF

echo "✓ Manifest generated: $OUTPUT"

# Validate JSON
if command -v jq &> /dev/null; then
    if jq empty "$OUTPUT" 2>/dev/null; then
        echo "✓ JSON validation passed"
    else
        echo "✗ JSON validation failed"
        exit 1
    fi
else
    echo "⚠ jq not found, skipping JSON validation"
fi
