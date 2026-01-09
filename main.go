package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/fernet/fernet-go"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
		Port    int    `yaml:"port"`
	} `yaml:"server"`
	Security struct {
		FernetKey string `yaml:"fernet_key"`
	} `yaml:"security"`
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	tmpl   *template.Template
	config Config
)

type SSHCredentials struct {
	Host       string
	User       string
	Password   string
	PrivateKey string
}

func init() {
	// Load configuration
	if err := loadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate Fernet key
	if config.Security.FernetKey == "" {
		log.Fatal("Fernet key is not configured. Please set security.fernet_key in config.yaml")
	}
	if _, err := fernet.DecodeKeys(config.Security.FernetKey); err != nil {
		log.Fatalf("Invalid Fernet key in config: %v", err)
	}

	// Load templates
	var err error
	tmpl, err = template.ParseGlob("templates/*.html")
	if err != nil {
		log.Printf("Warning: could not parse templates: %v", err)
	}
}

func loadConfig(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening config file: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("error reading config file: %v", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("error parsing config file: %v", err)
	}

	return nil
}

func main() {
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/terminal", terminalHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/static/", noCacheStaticHandler)

	addr := fmt.Sprintf("%s:%d", config.Server.Address, config.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func noCacheStaticHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	http.StripPrefix("/static/", http.FileServer(http.Dir("static"))).ServeHTTP(w, r)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	var creds SSHCredentials

	// Check for direct access via 'access' parameter
	accessParam := r.URL.Query().Get("access")
	if accessParam != "" {
		var err error
		creds, err = decryptAccess(accessParam)
		if err != nil {
			http.Error(w, "Invalid access token", http.StatusBadRequest)
			log.Printf("Failed to decrypt access token: %v", err)
			return
		}

		// Direct access mode - render terminal page directly
		if tmpl != nil {
			tmpl.ExecuteTemplate(w, "terminal.html", creds)
		} else {
			http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		}
		return
	}

	// Normal mode - render the form page
	if tmpl != nil {
		tmpl.ExecuteTemplate(w, "index.html", creds)
	} else {
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
	}
}

func terminalHandler(w http.ResponseWriter, r *http.Request) {
	// Render the terminal popup page
	if tmpl != nil {
		tmpl.ExecuteTemplate(w, "terminal.html", nil)
	} else {
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (max 2GB)
	r.ParseMultipartForm(2 << 30) // 2GB

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"success": false,
			"error":   "Failed to read file: " + err.Error(),
		})
		return
	}
	defer file.Close()

	// Get SSH credentials from form
	host := r.FormValue("host")
	user := r.FormValue("user")
	password := r.FormValue("password")
	privateKeyB64 := r.FormValue("privatekey")

	var privateKey []byte
	if privateKeyB64 != "" {
		privateKey, err = base64.StdEncoding.DecodeString(privateKeyB64)
		if err != nil {
			respondJSON(w, map[string]interface{}{
				"success": false,
				"error":   "Invalid private key encoding",
			})
			return
		}
	}

	// Upload file via SSH
	remotePath, err := uploadFileViaSSH(file, header.Filename, host, user, password, privateKey)
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"path":    remotePath,
	})
}

func respondJSON(w http.ResponseWriter, data map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Get parameters from query string
	host := r.URL.Query().Get("host")
	user := r.URL.Query().Get("user")
	password := r.URL.Query().Get("password")
	privateKeyB64 := r.URL.Query().Get("privatekey")
	remotePath := r.URL.Query().Get("path")

	if host == "" || user == "" || remotePath == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// Validate remote path - only allow downloads from /home, /opt, and /tmp
	allowedPaths := []string{"/home/", "/opt/", "/tmp/"}
	isAllowed := false
	for _, prefix := range allowedPaths {
		if strings.HasPrefix(remotePath, prefix) {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		http.Error(w, "Access denied: Downloads are only allowed from /home, /opt, and /tmp directories", http.StatusForbidden)
		return
	}

	var privateKey []byte
	var err error
	if privateKeyB64 != "" {
		privateKey, err = base64.StdEncoding.DecodeString(privateKeyB64)
		if err != nil {
			http.Error(w, "Invalid private key encoding", http.StatusBadRequest)
			return
		}
	}

	// Stream file from SSH server directly to response
	filename, err := downloadFileViaSSH(w, remotePath, host, user, password, privateKey)
	if err != nil {
		log.Printf("Download failed: %v", err)
		http.Error(w, "Download failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
}

func decryptAccess(encrypted string) (SSHCredentials, error) {
	var creds SSHCredentials

	// Get and validate Fernet key
	fernetKey := getDefaultFernetKey()
	if fernetKey == "" {
		return creds, fmt.Errorf("fernet key not configured")
	}

	// Decode the Fernet token
	key, err := fernet.DecodeKeys(fernetKey)
	if err != nil {
		return creds, fmt.Errorf("invalid fernet key: %v", err)
	}

	token_64 := fernet.VerifyAndDecrypt([]byte(encrypted), 0, key)
	if token_64 == nil {
		return creds, fmt.Errorf("failed to decrypt access token")
	}

	token, err := base64.StdEncoding.DecodeString(string(token_64))
	if err != nil {
		return creds, err
	}

	// Parse the decrypted data
	values, err := url.ParseQuery(string(token))
	if err != nil {
		return creds, err
	}

	creds.User = values.Get("username")
	creds.Host = values.Get("hostname")
	creds.PrivateKey = values.Get("privatekey")

	return creds, nil
}

// getDefaultFernetKey returns the Fernet key from configuration
func getDefaultFernetKey() string {
	return config.Security.FernetKey
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Get credentials from query params or initial message
	host := r.URL.Query().Get("host")
	user := r.URL.Query().Get("user")
	password := r.URL.Query().Get("password")
	privateKeyB64 := r.URL.Query().Get("privatekey")

	var privateKey []byte
	if privateKeyB64 != "" {
		privateKey, err = base64.StdEncoding.DecodeString(privateKeyB64)
		if err != nil {
			log.Printf("Failed to decode private key: %v", err)
			conn.WriteMessage(websocket.TextMessage, []byte("Error: Invalid private key encoding"))
			return
		}
	}

	// If no credentials in query, wait for initial message with credentials
	if host == "" {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Failed to read credentials: %v", err)
			return
		}

		// Parse credentials from message (format: host|user|password|privatekey_base64)
		parts := strings.Split(string(msg), "|")
		if len(parts) >= 2 {
			host = parts[0]
			user = parts[1]
			if len(parts) > 2 {
				password = parts[2]
			}
			if len(parts) > 3 && parts[3] != "" {
				privateKey, _ = base64.StdEncoding.DecodeString(parts[3])
			}
		}
	}

	if host == "" || user == "" {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: Missing host or user"))
		return
	}

	// Handle SSH connection
	handleSSHConnection(conn, host, user, password, privateKey)
}
