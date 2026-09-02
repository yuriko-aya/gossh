# Web SSH Bastion

A secure web-based SSH bastion/gateway built with Go, WebSocket, and xterm.js.

**GitHub Repository**: https://github.com/yuriko-aya/gossh

## Features

- **Web-based SSH Terminal**: Full terminal emulation using xterm.js
- **Token-based Access**: Encrypted Fernet URLs embed SSH credentials — no login form required
- **Real-time Communication**: WebSocket-based bidirectional communication
- **Authentication**: Supports both password and private key authentication
- **File Upload/Download**: S3-mediated uploads and SSH streaming downloads
- **Rate Limiting**: Per-IP limits on connect, WebSocket, and presign endpoints

## Prerequisites

- Go 1.16 or higher
- Modern web browser
- Python 3 with `cryptography` package (for URL generation)
- AWS S3 bucket (for file uploads)

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

4. Edit `config.yaml` and set your configuration (see [Configuration](#configuration) below).

## Running the Server

```bash
go run .
```

The server starts on the address and port in `config.yaml` (default: `http://localhost:8088`).

## Usage

Access is token-based. Generate an encrypted URL with the included script, then open it in a browser.

```bash
python3 generate_url.py --user admin --host 192.168.1.100 --password mypass
python3 generate_url.py --user admin --host 192.168.1.100 --key ~/.ssh/id_rsa
```

This prints a URL like `http://localhost:8088/?access=<token>`. Opening it launches the terminal directly.

The script reads the Fernet key from `config.yaml` (or the `FERNET_KEY` environment variable). Use `--fernet-key` to override.

### Token Format

Credentials inside the token use these query parameter names:

| Parameter | Description |
|-----------|-------------|
| `username` | SSH username |
| `hostname` | SSH host (optionally with port) |
| `password` | SSH password (optional) |
| `privatekey` | Base64-encoded private key (optional) |

## File Transfer

### Upload (S3-mediated)

Large files bypass the gossh server entirely:

1. Browser requests a presigned S3 PUT URL from `/presign`
2. Browser uploads directly to S3
3. gossh runs `wget` on the remote host via SSH to pull the file into `/tmp/`

### Download (SSH streaming)

Downloads are restricted to `/home/`, `/opt/`, `/tmp/`, and `/var/log/`. Use the **Download File** button in the terminal UI.

## Configuration

```yaml
server:
  address: 0.0.0.0
  port: 8088

security:
  fernet_key: your-key-here
  token_ttl: 24h          # Access token lifetime

rate_limit:
  connect_per_minute: 10
  ws_per_minute: 20
  presign_per_minute: 30

s3:
  region: us-east-1
  bucket: your-bucket-name
  access_key_id: YOUR_AWS_ACCESS_KEY_ID
  secret_access_key: YOUR_AWS_SECRET_ACCESS_KEY
```

### Environment Variable Overrides

These take precedence over `config.yaml`:

| Variable | Overrides |
|----------|-----------|
| `FERNET_KEY` | `security.fernet_key` |
| `S3_REGION` | `s3.region` |
| `S3_BUCKET` | `s3.bucket` |
| `S3_ACCESS_KEY_ID` | `s3.access_key_id` |
| `S3_SECRET_ACCESS_KEY` | `s3.secret_access_key` |
| `GOSSH_CONFIG` | Config file path (default: `config.yaml`) |

### Generate Fernet Key

```bash
python3 generate_url.py --generate-key
```

## S3 Bucket Setup

Uploads use presigned URLs under the `uploads/` prefix. Configure your bucket as follows.

### IAM Policy

Grant the credentials in `config.yaml` these permissions on the bucket:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject"],
      "Resource": "arn:aws:s3:::your-bucket-name/uploads/*"
    }
  ]
}
```

### Lifecycle Rule

Add a lifecycle rule to expire temporary upload objects (recommended: 1 day):

1. Open the bucket in the AWS Console → **Management** → **Lifecycle rules**
2. Create a rule scoped to prefix `uploads/`
3. Add an expiration action: **Expire current versions of objects** after **1 day**

This prevents orphaned upload objects from accumulating.

## Security Considerations

⚠️ **WARNING**: Review before production use:

1. Replace `ssh.InsecureIgnoreHostKey()` with proper host key verification
2. Use environment variables for secrets (`FERNET_KEY`, S3 credentials)
3. Use HTTPS/WSS in production (see [DEPLOYMENT.md](DEPLOYMENT.md))
4. Access tokens expire after `token_ttl` — adjust as needed
5. Rate limiting is enabled by default on sensitive endpoints
6. Implement proper logging and monitoring

## Production Deployment

See [DEPLOYMENT.md](DEPLOYMENT.md) for building, systemd setup, Nginx reverse proxy, SSL/TLS, and security hardening.

## Project Structure

```
gossh/
├── main.go              # HTTP server and handlers
├── auth.go              # Fernet token encrypt/decrypt
├── config.go            # Config loading and env overrides
├── ssh.go               # SSH session and file transfer
├── ssh_client.go        # Shared SSH dial helper
├── s3.go                # S3 presigned URLs
├── paths.go             # Download path whitelist
├── ratelimit.go         # Per-IP rate limiting
├── generate_url.py      # URL generation script
├── templates/
│   ├── index.html       # Blank landing page
│   └── terminal.html    # Terminal UI
├── config.yaml.example  # Configuration template
├── gossh.service        # systemd service file
├── nginx-gossh.conf     # Nginx configuration
├── deploy.sh            # Deployment script
├── build.sh             # Build script
└── DEPLOYMENT.md        # Deployment guide
```

## Dependencies

- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket implementation
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) — SSH client
- [fernet/fernet-go](https://github.com/fernet/fernet-go) — Fernet encryption
- [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2) — S3 presigned URLs
- [xterm.js](https://xtermjs.org/) — Terminal emulator (CDN)

## License

GPLv2 — See [LICENSE](LICENSE) file for details
