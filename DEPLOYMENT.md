# Production Deployment Guide

## Prerequisites

- Linux server (Ubuntu/Debian/CentOS)
- Go 1.16+ (for building) or use pre-built binary
- systemd (for service management)
- nginx (recommended for reverse proxy)
- Valid SSL certificate (recommended)

## Deployment Steps

### 1. Build the Application

On your development machine or CI/CD pipeline:

```bash
# Clone the repository
git clone https://github.com/yuriko-aya/gossh.git
cd gossh

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o gossh -ldflags="-s -w"

# Or use the build script
./build.sh
```

### 2. Prepare the Server

Create application directory and user:

```bash
# Create application user (no login shell for security)
sudo useradd -r -s /bin/false gossh

# Create application directory
sudo mkdir -p /opt/gossh
sudo mkdir -p /opt/gossh/templates
sudo mkdir -p /opt/gossh/static
sudo mkdir -p /var/log/gossh

# Set ownership
sudo chown -R gossh:gossh /opt/gossh
sudo chown -R gossh:gossh /var/log/gossh
```

### 3. Deploy Files

#### Option A: Deploy from Git (Recommended)

```bash
# On the server, clone the repository
sudo -u gossh git clone https://github.com/yuriko-aya/gossh.git /opt/gossh
cd /opt/gossh

# Build on server (requires Go installed)
sudo -u gossh GOOS=linux GOARCH=amd64 go build -o gossh -ldflags="-s -w"

# Or copy pre-built binary
sudo cp /tmp/gossh /opt/gossh/gossh
sudo chmod +x /opt/gossh/gossh
sudo chown gossh:gossh /opt/gossh/gossh

# Copy example config
sudo -u gossh cp config.yaml.example config.yaml
```

#### Option B: Deploy Pre-built Files

Copy files to the server:

```bash
# From your local machine
scp -r gossh templates static config.yaml.example gossh.service deploy.sh user@server:/tmp/

# On the server
sudo ./deploy.sh
```

#### Option C: Manual Deployment

Copy files to the server:

```bash
# Copy binary
sudo cp gossh /opt/gossh/

# Copy templates
sudo cp -r templates/* /opt/gossh/templates/

# Copy static files
sudo cp -r static/* /opt/gossh/static/

# Copy configuration
sudo cp config.yaml /opt/gossh/

# Set permissions
sudo chmod +x /opt/gossh/gossh
sudo chown -R gossh:gossh /opt/gossh
```

### 4. Configure the Application

Edit `/opt/gossh/config.yaml`:

```yaml
server:
  # Bind to localhost if using reverse proxy, or 0.0.0.0 if direct
  address: 127.0.0.1
  port: 8088

security:
  # Generate a new key: python3 -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
  fernet_key: YOUR_PRODUCTION_KEY_HERE
```

**IMPORTANT**: Always generate a new Fernet key for production!

### 5. Install as systemd Service

Copy the service file:

```bash
sudo cp gossh.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable gossh
sudo systemctl start gossh
```

Check status:

```bash
sudo systemctl status gossh
sudo journalctl -u gossh -f  # Follow logs
```

### 6. Configure Nginx Reverse Proxy (Recommended)

Install nginx:

```bash
sudo apt install nginx  # Ubuntu/Debian
# or
sudo yum install nginx  # CentOS
```

Copy nginx configuration:

```bash
sudo cp nginx-gossh.conf /etc/nginx/sites-available/gossh
sudo ln -s /etc/nginx/sites-available/gossh /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### 7. SSL/TLS Configuration (Highly Recommended)

Using Let's Encrypt (Certbot):

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d yourdomain.com
```

The nginx configuration template includes SSL settings that will be activated by Certbot.

## Security Best Practices

### 1. Firewall Configuration

```bash
# Allow only necessary ports
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP (for Let's Encrypt)
sudo ufw allow 443/tcp   # HTTPS
sudo ufw enable

# Block direct access to application port if using reverse proxy
sudo ufw deny 8088/tcp
```

### 2. File Permissions

```bash
# Ensure proper permissions
sudo chmod 600 /opt/gossh/config.yaml
sudo chmod 755 /opt/gossh/gossh
sudo chmod 755 /opt/gossh/templates
sudo chmod 755 /opt/gossh/static
```

### 3. SELinux (if enabled on CentOS/RHEL)

```bash
sudo semanage port -a -t http_port_t -p tcp 8088
sudo setsebool -P httpd_can_network_connect 1
```

### 4. Log Rotation

Create `/etc/logrotate.d/gossh`:

```
/var/log/gossh/*.log {
    daily
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 gossh gossh
    sharedscripts
    postrotate
        systemctl reload gossh > /dev/null 2>&1 || true
    endscript
}
```

## Monitoring

### Check Application Status

```bash
sudo systemctl status gossh
sudo journalctl -u gossh --since today
```

### Monitor Logs

```bash
# Follow application logs
sudo journalctl -u gossh -f

# Follow nginx logs
sudo tail -f /var/log/nginx/access.log
sudo tail -f /var/log/nginx/error.log
```

## Updating the Application

### Using Git (Recommended)

```bash
# Stop the service
sudo systemctl stop gossh

# Update from git
cd /opt/gossh
sudo -u gossh git pull

# Rebuild
sudo -u gossh go build -o gossh -ldflags="-s -w"

# Restart service
sudo systemctl start gossh
sudo systemctl status gossh
```

### Using Pre-built Binary

```bash
# Stop the service
sudo systemctl stop gossh

# Backup current binary
sudo cp /opt/gossh/gossh /opt/gossh/gossh.backup.$(date +%Y%m%d_%H%M%S)

# Deploy new binary
sudo cp gossh /opt/gossh/
sudo chmod +x /opt/gossh/gossh
sudo chown gossh:gossh /opt/gossh/gossh

# Start the service
sudo systemctl start gossh

# Check status
sudo systemctl status gossh
```

## Troubleshooting

### Service won't start

```bash
# Check logs
sudo journalctl -u gossh -n 50

# Check config
sudo -u gossh /opt/gossh/gossh --help

# Test manually
sudo -u gossh /opt/gossh/gossh
```

### Permission issues

```bash
# Verify ownership
ls -la /opt/gossh

# Fix if needed
sudo chown -R gossh:gossh /opt/gossh
```

### Connection issues

```bash
# Check if service is listening
sudo netstat -tlnp | grep 8088

# Check nginx
sudo nginx -t
sudo systemctl status nginx
```

## Backup

Regular backup of important files:

```bash
#!/bin/bash
BACKUP_DIR="/backup/gossh"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
tar -czf $BACKUP_DIR/gossh-$DATE.tar.gz \
    /opt/gossh/config.yaml \
    /opt/gossh/templates \
    /opt/gossh/static

# Keep only last 30 days
find $BACKUP_DIR -name "gossh-*.tar.gz" -mtime +30 -delete
```

## Performance Tuning

### Nginx

Edit `/etc/nginx/nginx.conf`:

```nginx
worker_processes auto;
worker_connections 1024;
keepalive_timeout 65;
```

### System Limits

Edit `/etc/security/limits.conf`:

```
gossh soft nofile 65536
gossh hard nofile 65536
```

## Production Checklist

- [ ] New Fernet key generated
- [ ] Config file has correct address/port
- [ ] Service user created (no shell)
- [ ] Proper file permissions set
- [ ] systemd service enabled
- [ ] Nginx reverse proxy configured
- [ ] SSL/TLS certificate installed
- [ ] Firewall rules configured
- [ ] Log rotation configured
- [ ] Monitoring setup
- [ ] Backup script scheduled
- [ ] Documentation updated with deployment details
