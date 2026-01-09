#!/bin/bash
# Deployment script for Go SSH Web Terminal

set -e

# Configuration
APP_NAME="gossh"
APP_DIR="/opt/gossh"
SERVICE_NAME="gossh.service"
APP_USER="gossh"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting deployment of $APP_NAME...${NC}"

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}Please run as root or with sudo${NC}"
    exit 1
fi

# Create user if doesn't exist
if ! id "$APP_USER" &>/dev/null; then
    echo -e "${YELLOW}Creating user $APP_USER...${NC}"
    useradd -r -s /bin/false $APP_USER
fi

# Create directories
echo -e "${YELLOW}Creating directories...${NC}"
mkdir -p $APP_DIR/templates
mkdir -p $APP_DIR/static
mkdir -p /var/log/gossh

# Stop service if running
if systemctl is-active --quiet $APP_NAME; then
    echo -e "${YELLOW}Stopping $APP_NAME service...${NC}"
    systemctl stop $APP_NAME
fi

# Backup existing binary
if [ -f "$APP_DIR/gossh" ]; then
    echo -e "${YELLOW}Backing up existing binary...${NC}"
    cp $APP_DIR/gossh $APP_DIR/gossh.backup.$(date +%Y%m%d_%H%M%S)
fi

# Copy files
echo -e "${YELLOW}Copying application files...${NC}"
cp gossh $APP_DIR/
cp -r templates/* $APP_DIR/templates/
cp -r static/* $APP_DIR/static/

# Copy config if it doesn't exist
if [ ! -f "$APP_DIR/config.yaml" ]; then
    echo -e "${YELLOW}Creating default config...${NC}"
    cp config.yaml.example $APP_DIR/config.yaml
    echo -e "${RED}IMPORTANT: Edit $APP_DIR/config.yaml and set production values!${NC}"
fi

# Set permissions
echo -e "${YELLOW}Setting permissions...${NC}"
chmod +x $APP_DIR/gossh
chmod 600 $APP_DIR/config.yaml
chown -R $APP_USER:$APP_USER $APP_DIR
chown -R $APP_USER:$APP_USER /var/log/gossh

# Install systemd service
echo -e "${YELLOW}Installing systemd service...${NC}"
cp $SERVICE_NAME /etc/systemd/system/
systemctl daemon-reload
systemctl enable $APP_NAME

# Start service
echo -e "${YELLOW}Starting $APP_NAME service...${NC}"
systemctl start $APP_NAME

# Check status
sleep 2
if systemctl is-active --quiet $APP_NAME; then
    echo -e "${GREEN}✓ Service started successfully${NC}"
    systemctl status $APP_NAME --no-pager
else
    echo -e "${RED}✗ Service failed to start${NC}"
    journalctl -u $APP_NAME -n 20 --no-pager
    exit 1
fi

echo ""
echo -e "${GREEN}Deployment completed!${NC}"
echo ""
echo "Next steps:"
echo "1. Edit $APP_DIR/config.yaml with production settings"
echo "2. Generate new Fernet key: python3 -c \"from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())\""
echo "3. Configure nginx reverse proxy (see nginx-gossh.conf)"
echo "4. Setup SSL with certbot: certbot --nginx -d yourdomain.com"
echo "5. Configure firewall"
echo ""
echo "Useful commands:"
echo "  sudo systemctl status $APP_NAME"
echo "  sudo journalctl -u $APP_NAME -f"
echo "  sudo systemctl restart $APP_NAME"
