#!/bin/bash
# Build script for production deployment

set -e

install_path="/opt/gossh"
path_owner="gossh"
path_group="gossh"
gossh_service="gossh.service"

# Make sure the script is run as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root or with sudo"
    exit 1
fi

echo "Building Go SSH Web Terminal..."

# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o gossh -ldflags="-s -w"
if [ $? -ne 0 ]; then
    echo "Build failed!"
    exit 1
fi

if [[ ! -d "$install_path" ]]; then
    echo "Install directory $install_path does not exist. Skipping installation."
    exit 0
fi

echo "Installing gossh to $install_path..."

if systemctl is-active --quiet $gossh_service; then
    echo "Stopping gossh service..."
    systemctl stop $gossh_service
    install -m 755 -o $path_owner -g $path_group gossh $install_path/gossh
    systemctl start $gossh_service
    echo "gossh service restarted successfully."
else
    install -m 755 -o $path_owner -g $path_group gossh $install_path/gossh
    if systemctl is-enabled --quiet $gossh_service; then
        systemctl start $gossh_service
        echo "gossh service started successfully."
    else
        echo "Please enable the gossh service to start on boot: sudo systemctl enable $gossh_service"
    fi
fi