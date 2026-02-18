#!/bin/bash

# Build and install Celeste binary to PATH
# This script builds the binary and copies it to ~/.local/bin automatically

set -e

echo "🔨 Building Celeste..."
go build -o Celeste main.go scaffolding.go animation.go ui.go assets.go interactive.go terminal_display.go ascii_art.go

echo "📦 Installing to PATH..."
cp Celeste ~/.local/bin/Celeste
chmod +x ~/.local/bin/Celeste

echo "✅ Celeste updated in PATH"
echo "📍 Location: ~/.local/bin/Celeste"
echo "🎯 Test with: Celeste --version"
