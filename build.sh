#!/bin/bash
# Build script for production deployment

set -e

echo "Building Go SSH Web Terminal..."

# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o gossh -ldflags="-s -w"

echo "Build complete: gossh"
echo "Size: $(du -h gossh | cut -f1)"
echo ""
echo "To deploy to production server:"
echo "1. Copy all files: scp -r gossh templates static config.yaml.example gossh.service deploy.sh user@server:/tmp/"
echo "2. On server: cd /tmp && sudo ./deploy.sh"
