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

	// Create remote file path
	remotePath := fmt.Sprintf("/tmp/%s", msg.Filename)

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

	// Constrain destination to /tmp/ to prevent path traversal
	dest := filepath.Clean(msg.Dest)
	if !strings.HasPrefix(dest, "/tmp/") {
		response.Success = false
		response.Error = "destination must be within /tmp/"
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

func uploadFileViaSSH(file multipart.File, filename, host, user, password string, privateKey []byte) (string, error) {
	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		return "", err
	}
	defer sshConn.Close()

	// Create remote file path
	remotePath := fmt.Sprintf("/tmp/%s", filename)

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

func validateFileViaSSH(remotePath, host, user, password string, privateKey []byte) (map[string]interface{}, error) {
	if !isAllowedDownloadPath(remotePath) {
		return nil, fmt.Errorf("access denied: downloads are only allowed from /home, /opt, /var/log, and /tmp directories")
	}

	sshConn, err := dialSSH(host, user, password, privateKey)
	if err != nil {
		return nil, err
	}
	defer sshConn.Close()

	// Create a session to check file
	session, err := sshConn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	// Check if file exists and get its size using stat
	output, err := session.CombinedOutput(fmt.Sprintf("test -f '%s' && stat -c '%%s' '%s' || echo 'NOT_FOUND'", remotePath, remotePath))
	if err != nil || strings.TrimSpace(string(output)) == "NOT_FOUND" {
		return nil, fmt.Errorf("file not found or not a regular file: %s", remotePath)
	}

	// Parse file size
	var fileSize int64
	if _, err := fmt.Sscanf(string(output), "%d", &fileSize); err != nil {
		return nil, fmt.Errorf("failed to get file information: %v", err)
	}

	// Extract filename from path
	filename := filepath.Base(remotePath)

	return map[string]interface{}{
		"filename": filename,
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

	// Extract filename from path
	filename := filepath.Base(remotePath)

	// Get file size first using stat command
	statSession, err := sshConn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create stat session: %v", err)
	}
	statOutput, err := statSession.CombinedOutput(fmt.Sprintf("stat -c %%s '%s'", remotePath))
	statSession.Close()
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %v", err)
	}

	// Parse file size
	var fileSize int64
	if _, err := fmt.Sscanf(string(statOutput), "%d", &fileSize); err != nil {
		return "", fmt.Errorf("failed to parse file size: %v", err)
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
