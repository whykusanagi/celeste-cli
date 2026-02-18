#!/bin/bash

# Celeste Installation Script
# This script installs Celeste to your PATH
# Animation is embedded in the binary - no external assets needed

set -e

echo "🚀 Installing Celeste..."

# Check if Celeste binary exists
if [ ! -f "./Celeste" ]; then
    echo "❌ Error: Celeste binary not found in current directory"
    echo "Please run this script from the celesteCLI directory"
    exit 1
fi

# Check if personality.yml exists
if [ ! -f "./personality.yml" ]; then
    echo "⚠️  Warning: personality.yml not found in current directory"
    echo "The CLI will work but personality features may be limited"
else
    # Create ~/.celeste config directory if it doesn't exist
    mkdir -p ~/.celeste

    # Copy personality.yml to config directory
    cp personality.yml ~/.celeste/
    echo "✅ Copied personality.yml to ~/.celeste/"
fi

# Create ~/.local/bin if it doesn't exist
mkdir -p ~/.local/bin

# Copy Celeste to ~/.local/bin
cp Celeste ~/.local/bin/

# Make it executable
chmod +x ~/.local/bin/Celeste

# Check if ~/.local/bin is in PATH
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    echo "⚠️  Warning: ~/.local/bin is not in your PATH"
    echo "Add this line to your ~/.bashrc, ~/.zshrc, or ~/.profile:"
    echo "export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "Then run: source ~/.bashrc (or ~/.zshrc)"
fi

# Test installation
if command -v Celeste &> /dev/null; then
    echo "✅ Celeste installed successfully!"
    echo "📍 Location: $(which Celeste)"
    echo ""
    echo "🎯 Test it with: Celeste --help"
else
    echo "❌ Installation failed or PATH not updated"
    echo "Try running: source ~/.bashrc (or ~/.zshrc)"
fi
