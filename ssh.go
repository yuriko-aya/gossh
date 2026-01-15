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
}

type UploadResponse struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Error   string `json:"error"`
}

func handleSSHConnection(wsConn *websocket.Conn, host, user, password string, privateKey []byte) {
	// Build SSH client configuration
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // WARNING: Use proper host key verification in production
	}

	// Add authentication methods
	if password != "" {
		config.Auth = append(config.Auth, ssh.Password(password))
	}

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			log.Printf("Failed to parse private key: %v", err)
			wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to parse private key: %v\r\n", err)))
			return
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	// Add default port if not specified
	if !containsPort(host) {
		host = host + ":22"
	}

	// Connect to SSH server
	sshConn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		log.Printf("Failed to connect to SSH server: %v", err)
		wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to connect: %v\r\n", err)))
		return
	}
	defer sshConn.Close()

	// Create SSH session
	session, err := sshConn.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to create session: %v\r\n", err)))
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
		wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to request PTY: %v\r\n", err)))
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
		wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: Failed to start shell: %v\r\n", err)))
		return
	}

	// Handle SSH output to WebSocket
	done := make(chan bool)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading stdout: %v", err)
				}
				done <- true
				return
			}
			if n > 0 {
				wsConn.WriteMessage(websocket.BinaryMessage, buf[:n])
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
				wsConn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}
	}()

	// Handle WebSocket input to SSH
	go func() {
		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				log.Printf("Error reading from websocket: %v", err)
				stdin.Close()
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}

			switch msg.Type {
			case "input":
				// Write user input to SSH stdin
				if _, err := stdin.Write([]byte(msg.Data)); err != nil {
					log.Printf("Error writing to stdin: %v", err)
					return
				}
			case "resize":
				// Resize terminal
				if err := session.WindowChange(msg.Rows, msg.Cols); err != nil {
					log.Printf("Error resizing terminal: %v", err)
				}
			case "upload":
				// Handle file upload
				go handleFileUpload(wsConn, sshConn, msg)
			}
		}
	}()

	// Wait for session to finish or stdout to close
	<-done
	log.Println("SSH session ended")

	// Wait for session to finish
	session.Wait()

	// Close the WebSocket connection
	wsConn.Close()
}

func containsPort(host string) bool {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return true
		}
		if host[i] == ']' {
			// IPv6 address without port
			return false
		}
	}
	return false
}

func handleFileUpload(wsConn *websocket.Conn, sshConn *ssh.Client, msg WSMessage) {
	var response UploadResponse
	response.Type = "upload_response"

	// Decode base64 file data
	fileData, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to decode file data: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}

	// Create remote file path
	remotePath := fmt.Sprintf("/tmp/%s", msg.Filename)

	// Create a new session to write the file
	uploadSession, err := sshConn.NewSession()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to create upload session: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}
	defer uploadSession.Close()

	// Get stdin pipe
	stdinPipe, err := uploadSession.StdinPipe()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to get stdin pipe: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}

	// Get stderr to capture any errors
	stderrPipe, err := uploadSession.StderrPipe()
	if err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to get stderr pipe: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}

	// Use cat to write the file - properly quote the filename to handle spaces and special characters
	if err := uploadSession.Start(fmt.Sprintf("cat > '%s'", remotePath)); err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to start upload command: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}

	// Write file data
	if _, err := stdinPipe.Write(fileData); err != nil {
		response.Success = false
		response.Error = fmt.Sprintf("Failed to write file data: %v", err)
		sendUploadResponse(wsConn, response)
		return
	}
	stdinPipe.Close()

	// Wait for command to complete and check for errors
	if err := uploadSession.Wait(); err != nil {
		// Read stderr to get error details
		stderrData, _ := io.ReadAll(stderrPipe)
		response.Success = false
		response.Error = fmt.Sprintf("Failed to upload file: %v - %s", err, string(stderrData))
		sendUploadResponse(wsConn, response)
		return
	}

	response.Success = true
	response.Path = remotePath
	sendUploadResponse(wsConn, response)
}

func sendUploadResponse(wsConn *websocket.Conn, response UploadResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal upload response: %v", err)
		return
	}

	if err := wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("Failed to send upload response: %v", err)
	}
}

func uploadFileViaSSH(file multipart.File, filename, host, user, password string, privateKey []byte) (string, error) {
	// Build SSH client configuration
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Add authentication methods
	if password != "" {
		config.Auth = append(config.Auth, ssh.Password(password))
	}

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return "", fmt.Errorf("failed to parse private key: %v", err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	// Add default port if not specified
	if !containsPort(host) {
		host = host + ":22"
	}

	// Connect to SSH server
	sshConn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return "", fmt.Errorf("failed to connect to SSH server: %v", err)
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

func downloadFileViaSSH(w http.ResponseWriter, remotePath, host, user, password string, privateKey []byte) (string, error) {
	// Validate remote path - only allow downloads from /home, /opt, and /tmp
	allowedPaths := []string{"/home/", "/opt/", "/tmp/"}
	isAllowed := false
	for _, prefix := range allowedPaths {
		if len(remotePath) >= len(prefix) && remotePath[:len(prefix)] == prefix {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return "", fmt.Errorf("access denied: downloads are only allowed from /home, /opt, and /tmp directories")
	}

	// Build SSH client configuration
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Add authentication methods
	if password != "" {
		config.Auth = append(config.Auth, ssh.Password(password))
	}

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return "", fmt.Errorf("failed to parse private key: %v", err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	// Add default port if not specified
	if !containsPort(host) {
		host = host + ":22"
	}

	// Connect to SSH server
	sshConn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return "", fmt.Errorf("failed to connect to SSH server: %v", err)
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

	// Set response headers before starting the stream
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "application/octet-stream")

	// Use cat to read the file - properly quote the filename
	if err := downloadSession.Start(fmt.Sprintf("cat '%s'", remotePath)); err != nil {
		return "", fmt.Errorf("failed to start download command: %v", err)
	}

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
