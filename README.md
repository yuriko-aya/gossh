# Web SSH Bastion

A secure web-based SSH bastion/gateway built with Go, WebSocket, and xterm.js.

**GitHub Repository**: https://github.com/yuriko-aya/gossh

## Features

- **Web-based SSH Terminal**: Full terminal emulation using xterm.js
- **Form-based Access**: Connect via a user-friendly web form
- **Direct Access**: URL-based access with encrypted credentials using Python Fernet
- **Real-time Communication**: WebSocket-based bidirectional communication
- **Authentication**: Supports both password and private key authentication
- **File Upload/Download**: Transfer files to/from remote servers

## Prerequisites

- Go 1.16 or higher
- Modern web browser

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yuriko-aya/gossh.git
cd gossh
```

2. Install dependencies:
```bash
go mod download
```

3. Configure the application:
```bash
cp config.yaml.example config.yaml
```

4. Edit `config.yaml` and set your configuration:
   - `server.address`: Server listen address (default: "0.0.0.0")
   - `server.port`: Server listen port (default: 8088)
   - `security.fernet_key`: Encryption key for access tokens (generate with: `python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"`)

## Running the Server

```bash
go run .
```

The server will start on the address and port specified in `config.yaml` (default: `http://localhost:8088`)

## Usage

### Form-based Access

1. Navigate to `http://localhost:8088`
2. Fill in the connection details:
   - Host: SSH server hostname or IP
   - Username: SSH username
   - Password: (optional) SSH password
   - Private Key: (optional) Upload your private key file
3. Click "Connect"

### Direct Access with Encrypted URL

Generate an encrypted access token using Python:

```python
from cryptography.fernet import Fernet
import base64

# Use the same key as configured in the Go application
key = b'cw_0x689RpI-jtRR7oE8h_eQsKImvJapLeSbXpwF4e4='
f = Fernet(key)

# Prepare credentials
data = f"user=myuser&host=example.com&privatekey={base64.b64encode(private_key_content).decode()}"

# Encrypt
token = f.encrypt(data.encode()).decode()

# Use in URL
print(f"http://localhost:8080/?access={token}")
```

## Configuration

All configuration is managed in `config.yaml`:

```yaml
server:
  address: 0.0.0.0
  port: 8088

security:
  fernet_key: your-key-here
```

### Generate Fernet Key

```python
from cryptography.fernet import Fernet
print(Fernet.generate_key().decode())
```

### Generate Access URL

Use the included `generate_url.py` script:

```bash
python3 generate_url.py --host 192.168.1.100 --user admin --password mypass
python3 generate_url.py --host 192.168.1.100 --user admin --key ~/.ssh/id_rsa
```

## Security Considerations

⚠️ **WARNING**: This is a demonstration project. For production use:

1. Replace `ssh.InsecureIgnoreHostKey()` with proper host key verification
2. Use environment variables for the Fernet key
3. Implement proper authentication and authorization
4. Use HTTPS/WSS in production
5. Add rate limiting and connection pooling
6. Implement proper logging and monitoring
7. Add session management and timeout handling

## Production Deployment

See [DEPLOYMENT.md](DEPLOYMENT.md) for comprehensive production deployment guide including:
- Building and deploying binaries
- systemd service setup
- Nginx reverse proxy configuration
- SSL/TLS setup with Let's Encrypt
- Security hardening
- Monitoring and logging

## Project Structure

```
gossh/
├── main.go              # HTTP server and handlers
├── ssh.go               # SSH connection logic
├── generate_url.py      # URL generation script
├── templates/
│   ├── index.html       # Login form page
│   └── terminal.html    # Terminal UI page
├── static/
│   └── app.js           # Frontend JavaScript
├── config.yaml.example  # Configuration template
├── gossh.service        # systemd service file
├── nginx-gossh.conf     # Nginx configuration
├── deploy.sh            # Deployment script
├── build.sh             # Build script
├── go.mod               # Go module definition
├── DEPLOYMENT.md        # Deployment guide
└── README.md            # This file
```

## Dependencies

- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) - SSH client
- [fernet/fernet-go](https://github.com/fernet/fernet-go) - Fernet encryption
- [xterm.js](https://xtermjs.org/) - Terminal emulator (CDN)

## License

GPLv2 - See [LICENSE](LICENSE) file for details
