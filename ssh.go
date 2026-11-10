package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type WSMessage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Key      string `json:"key"`  // S3 object key for s3_pull
	Dest     string `json:"dest"` // remote destination path for s3_pull
}

type UploadResponse struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Error   string `json:"error"`
}

func handleSSHConnection(wsConn *websocket.Conn, host, user, password string, privateKey []byte) {
	ws := newSafeWebSocket(wsConn)
	activeSessions.register(ws)
	defer activeSessions.unregister(ws)

	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		log.Printf("Failed to connect to SSH server: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to connect: %v\r\n", err)))
		return
	}
	defer sshConn.Close()

	// Create SSH session
	session, err := sshConn.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to create session: %v\r\n", err)))
		return
	}
	defer session.Close()

	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	// Request pseudo terminal
	if err := session.RequestPty("xterm-256color", 40, 80, modes); err != nil {
		log.Printf("Failed to request pseudo terminal: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to request PTY: %v\r\n", err)))
		return
	}

	// Set up pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		log.Printf("Failed to setup stdin pipe: %v", err)
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Printf("Failed to setup stdout pipe: %v", err)
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		log.Printf("Failed to setup stderr pipe: %v", err)
		return
	}

	// Start shell
	if err := session.Shell(); err != nil {
		log.Printf("Failed to start shell: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to start shell: %v\r\n", err)))
		return
	}

	sessionDone := make(chan struct{})
	keepaliveStop := make(chan struct{})
	var endOnce sync.Once
	endSession := func(reason string) {
		endOnce.Do(func() {
			log.Printf("Ending SSH session: %s", reason)
			stdin.Close()
			session.Close()
			close(keepaliveStop)
			close(sessionDone)
		})
	}

	startWebSocketKeepalive(ws, keepaliveStop, endSession)
	startSSHKeepalive(sshConn, keepaliveStop)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading stdout: %v", err)
				}
				endSession("stdout closed")
				return
			}
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					log.Printf("Error writing stdout to websocket: %v", err)
					endSession("websocket write failed")
					return
				}
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading stderr: %v", err)
				}
				return
			}
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					log.Printf("Error writing stderr to websocket: %v", err)
					endSession("websocket write failed")
					return
				}
			}
		}
	}()

	// Handle WebSocket input to SSH
	go func() {
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				log.Printf("Error reading from websocket: %v", err)
				endSession("websocket closed")
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}

			switch msg.Type {
			case "input":
				if _, err := stdin.Write([]byte(msg.Data)); err != nil {
					log.Printf("Error writing to stdin: %v", err)
					endSession("stdin write failed")
					return
				}
			case "resize":
				if err := session.WindowChange(msg.Rows, msg.Cols); err != nil {
					log.Printf("Error resizing terminal: %v", err)
				}
			case "upload":
				go handleFileUpload(ws, sshConn, msg)
			case "s3_pull":
				go handleS3Pull(ws, sshConn, msg)
			}
		}
	}()

	<-sessionDone
	session.Wait()
	ws.Close()
}

func handleFileUpload(ws *safeWebSocket, sshConn *ssh.Client, msg WSMessage) {
	var response UploadResponse
	response.Type = "upload_response"

	// Decode base64 file data
	fileData, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to decode file data: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	fileSize := int64(len(fileData))
	if msg.Size > 0 {
		fileSize = msg.Size
	}

	remotePath, err := resolveUploadDestination(sshConn, msg.Filename, fileSize)
	if err != nil {
		response.Success = false
		response.Error = err.Error()
		sendUploadResponse(ws, response)
		return
	}

	// Create a new session to write the file
	uploadSession, err := sshConn.NewSession()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to create upload session: %v", err)
		sendUploadResponse(ws, response)
		return
	}
	defer uploadSession.Close()

	// Get stdin pipe
	stdinPipe, err := uploadSession.StdinPipe()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to get stdin pipe: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	// Get stderr to capture any errors
	stderrPipe, err := uploadSession.StderrPipe()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to get stderr pipe: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	// Use cat to write the file - properly quote the filename to handle spaces and special characters
	if err := uploadSession.Start(fmt.Sprintf("cat > '%s'", remotePath)); err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to start upload command: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	// Write file data
	if _, err := stdinPipe.Write(fileData); err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to write file data: %v", err)
		sendUploadResponse(ws, response)
		return
	}
	stdinPipe.Close()

	// Wait for command to complete and check for errors
	if err := uploadSession.Wait(); err != nil {
		// Read stderr to get error details
		stderrData, _ := io.ReadAll(stderrPipe)
		response.Success = false
		response.Error = fmt.Sprintf("Failed to upload file: %v - %s", err, string(stderrData))
		sendUploadResponse(ws, response)
		return
	}

	response.Success = true
	response.Path = remotePath
	sendUploadResponse(ws, response)
}

func sendUploadResponse(ws *safeWebSocket, response UploadResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal upload response: %v", err)
		return
	}

	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("Failed to send upload response: %v", err)
	}
}

// handleS3Pull generates a presigned GET URL for the uploaded S3 object and
// uses wget over SSH to pull it onto the remote host.
func handleS3Pull(ws *safeWebSocket, sshConn *ssh.Client, msg WSMessage) {
	var response UploadResponse
	response.Type = "s3_pull_response"

	dest := filepath.Clean(msg.Dest)
	if !isAllowedUploadPath(dest) {
		response.Success = false
		response.Error = "destination must be within /tmp/ or /var/tmp/"
		sendUploadResponse(ws, response)
		return
	}

	// Generate a short-lived presigned GET URL — used immediately by wget
	getURL, err := presignGetURL(msg.Key, 5*time.Minute)
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("failed to generate download URL: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	pullSession, err := sshConn.NewSession()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("failed to create pull session: %v", err)
		sendUploadResponse(ws, response)
		return
	}
	defer pullSession.Close()

	stderrPipe, err := pullSession.StderrPipe()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("failed to get stderr pipe: %v", err)
		sendUploadResponse(ws, response)
		return
	}

	// Single-quoted strings are safe: S3 presigned URLs and /tmp/ paths
	// never contain single quotes.
	cmd := fmt.Sprintf("wget -q -O '%s' '%s'", dest, getURL)
	if err := pullSession.Run(cmd); err != nil {
		stderrData, _ := io.ReadAll(stderrPipe)
		response.Success = false
		response.Error = fmt.Sprintf("wget failed: %v - %s", err, string(stderrData))
		sendUploadResponse(ws, response)
		return
	}

	response.Success = true
	response.Path = dest
	sendUploadResponse(ws, response)
}

func uploadFileViaSSH(file multipart.File, filename string, fileSize int64, host, user, password string, privateKey []byte) (string, error) {
	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		return "", err
	}
	defer sshConn.Close()

	remotePath, err := resolveUploadDestination(sshConn, filename, fileSize)
	if err != nil {
		return "", err
	}

	// Create a new session to write the file
	uploadSession, err := sshConn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create upload session: %v", err)
	}
	defer uploadSession.Close()

	// Get stdin pipe
	stdinPipe, err := uploadSession.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdin pipe: %v", err)
	}

	// Get stderr to capture any errors
	stderrPipe, err := uploadSession.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %v", err)
	}

	// Use cat to write the file - properly quote the filename
	if err := uploadSession.Start(fmt.Sprintf("cat > '%s'", remotePath)); err != nil {
		return "", fmt.Errorf("failed to start upload command: %v", err)
	}

	// Copy file data to stdin
	if _, err := io.Copy(stdinPipe, file); err != nil {
		return "", fmt.Errorf("failed to write file data: %v", err)
	}
	stdinPipe.Close()

	// Wait for command to complete
	if err := uploadSession.Wait(); err != nil {
		stderrData, _ := io.ReadAll(stderrPipe)
		return "", fmt.Errorf("failed to upload file: %v - %s", err, string(stderrData))
	}

	return remotePath, nil
}

func downloadFileCheckCommand(remotePath string) string {
	return fmt.Sprintf(`p='%s'
out=$(LC_ALL=C stat -c '%%s' -- "$p" 2>&1) || {
  case "$out" in
    *"Permission denied"*) echo PERMISSION_DENIED; exit 0 ;;
    *) echo NOT_FOUND; exit 0 ;;
  esac
}
[ -f "$p" ] || { echo NOT_REGULAR; exit 0; }
[ -r "$p" ] || { echo PERMISSION_DENIED; exit 0; }
echo "OK $out"
`, remotePath)
}

func permissionDeniedDownloadError(remotePath string) error {
	return fmt.Errorf("permission denied: cannot access %s; copy it to /tmp (e.g. sudo cp '%s' /tmp/) then download from /tmp", remotePath, remotePath)
}

func fileNotFoundDownloadError(remotePath string) error {
	return fmt.Errorf("file not found or not a regular file: %s", remotePath)
}

func parseFileCheckOutput(output, remotePath string) (int64, error) {
	line := strings.TrimSpace(output)
	switch {
	case line == "PERMISSION_DENIED":
		return 0, permissionDeniedDownloadError(remotePath)
	case line == "NOT_FOUND", line == "NOT_REGULAR":
		return 0, fileNotFoundDownloadError(remotePath)
	case strings.HasPrefix(line, "OK "):
		var size int64
		if _, err := fmt.Sscanf(line, "OK %d", &size); err != nil {
			return 0, fmt.Errorf("failed to get file information: %v", err)
		}
		return size, nil
	default:
		if line == "" {
			return 0, fmt.Errorf("failed to get file information")
		}
		return 0, fmt.Errorf("failed to get file information: %s", line)
	}
}

func checkDownloadFile(sshConn *ssh.Client, remotePath string) (int64, error) {
	session, err := sshConn.NewSession()
	if err != nil {
		return 0, fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(downloadFileCheckCommand(remotePath))
	if err != nil {
		return 0, fmt.Errorf("failed to get file information: %v", err)
	}
	return parseFileCheckOutput(string(output), remotePath)
}

func validateFileViaSSH(remotePath, host, user, password string, privateKey []byte) (map[string]interface{}, error) {
	if !isAllowedDownloadPath(remotePath) {
		return nil, fmt.Errorf("access denied: downloads are only allowed from /home, /opt, /var/log, and /tmp directories")
	}

	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		return nil, err
	}
	defer sshConn.Close()

	fileSize, err := checkDownloadFile(sshConn, remotePath)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"filename": filepath.Base(remotePath),
		"size":     fileSize,
	}, nil
}

func downloadFileViaSSH(w http.ResponseWriter, remotePath, host, user, password string, privateKey []byte) (string, error) {
	if !isAllowedDownloadPath(remotePath) {
		return "", fmt.Errorf("access denied: downloads are only allowed from /home, /opt, /var/log, and /tmp directories")
	}

	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		return "", err
	}
	defer sshConn.Close()

	// Create a new session to read the file
	downloadSession, err := sshConn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create download session: %v", err)
	}
	defer downloadSession.Close()

	// Get stdout pipe to stream file content
	stdoutPipe, err := downloadSession.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	// Get stderr to capture any errors
	stderrPipe, err := downloadSession.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %v", err)
	}

	filename := filepath.Base(remotePath)

	fileSize, err := checkDownloadFile(sshConn, remotePath)
	if err != nil {
		return "", err
	}

	// Start the cat command - do this before setting headers
	// so if it fails, we can still return a proper HTTP error
	if err := downloadSession.Start(fmt.Sprintf("cat '%s'", remotePath)); err != nil {
		return "", fmt.Errorf("failed to start download command: %v", err)
	}

	// Now set response headers - after this point, we're committed to streaming
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))

	// Stream file content directly to HTTP response writer
	// This avoids loading the entire file into memory
	if _, err := io.Copy(w, stdoutPipe); err != nil {
		return "", fmt.Errorf("failed to stream file data: %v", err)
	}

	// Wait for command to complete
	if err := downloadSession.Wait(); err != nil {
		stderrData, _ := io.ReadAll(stderrPipe)
		return "", fmt.Errorf("failed to download file: %v - %s", err, string(stderrData))
	}

	return filename, nil
}
