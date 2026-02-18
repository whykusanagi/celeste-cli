# scripts/ - Build and Installation Utilities

This directory contains user-facing scripts for building and installing CelesteCLI.

## Available Scripts

### build-and-install.sh
Complete build and installation workflow:
- Builds the binary with go build
- Copies to ~/.local/bin/
- Sets executable permissions
- Verifies installation

Usage:
```bash
./scripts/build-and-install.sh
```

### install.sh
Installation script for copying pre-built binary:
- Assumes binary already exists
- Copies to ~/.local/bin/
- Sets permissions

Usage:
```bash
./scripts/install.sh
```

## Alternative: Use Makefile

For more control, use the Makefile in the root directory:
```bash
# Build only
make build

# Build and install
make install

# Clean build artifacts
make clean
```

## Alternative: Use go install

For Go users, install directly:
```bash
go install github.com/whykusanagi/celesteCLI/cmd/celeste@latest
```

See [README.md](../README.md) for full installation instructions.
