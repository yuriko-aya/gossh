package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	S3 struct {
		Region          string `yaml:"region"`
		Bucket          string `yaml:"bucket"`
		AccessKeyID     string `yaml:"access_key_id"`
		SecretAccessKey string `yaml:"secret_access_key"`
	} `yaml:"s3"`
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return u.Host == r.Host
		},
	}
	tmpl   *template.Template
	config Config
)

type SSHCredentials struct {
	Host        string
	User        string
	Password    string
	PrivateKey  string
	AccessToken string
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
	http.HandleFunc("/presign", presignHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/validate-download", validateDownloadHandler)
	http.HandleFunc("/connect", connectHandler)
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

		// Store the access token for use in WebSocket/download/upload
		creds.AccessToken = accessParam

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
	accessParam := r.URL.Query().Get("access")
	var creds SSHCredentials
	if accessParam != "" {
		var err error
		creds, err = decryptAccess(accessParam)
		if err != nil {
			http.Error(w, "Invalid access token", http.StatusBadRequest)
			log.Printf("Failed to decrypt access token in terminalHandler: %v", err)
			return
		}
		creds.AccessToken = accessParam
	}
	if tmpl != nil {
		tmpl.ExecuteTemplate(w, "terminal.html", creds)
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

	// Check if using access token
	accessParam := r.FormValue("access")
	var host, user, password string
	var privateKey []byte

	if accessParam != "" {
		// Decrypt access token to get credentials
		creds, err := decryptAccess(accessParam)
		if err != nil {
			respondJSON(w, map[string]interface{}{
				"success": false,
				"error":   "Invalid access token",
			})
			return
		}
		host = creds.Host
		user = creds.User
		password = creds.Password
		if creds.PrivateKey != "" {
			privateKey, _ = base64.StdEncoding.DecodeString(creds.PrivateKey)
		}
	} else {
		respondJSON(w, map[string]interface{}{
			"success": false,
			"error":   "access token required",
		})
		return
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

func presignHandler(w http.ResponseWriter, r *http.Request) {
	// Require a valid access token — confirms the caller has an active SSH session
	accessParam := r.URL.Query().Get("access")
	if accessParam == "" {
		http.Error(w, "access token required", http.StatusBadRequest)
		return
	}
	if _, err := decryptAccess(accessParam); err != nil {
		http.Error(w, "Invalid access token", http.StatusUnauthorized)
		return
	}

	filename := filepath.Base(r.URL.Query().Get("filename"))
	if filename == "" || filename == "." {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}

	// Generate a random prefix so keys never collide and can't be guessed
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	objectKey := fmt.Sprintf("uploads/%x/%s", randBytes, filename)

	uploadURL, err := presignPutURL(objectKey, 15*time.Minute)
	if err != nil {
		respondJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	respondJSON(w, map[string]interface{}{
		"upload_url": uploadURL,
		"object_key": objectKey,
	})
}

func validateDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// Check if using access token
	accessParam := r.URL.Query().Get("access")
	remotePath := r.URL.Query().Get("path")

	var host, user, password string
	var privateKey []byte
	var err error

	if accessParam != "" {
		// Decrypt access token to get credentials
		creds, err := decryptAccess(accessParam)
		if err != nil {
			respondJSON(w, map[string]interface{}{
				"valid": false,
				"error": "Invalid access token",
			})
			return
		}
		host = creds.Host
		user = creds.User
		password = creds.Password
		if creds.PrivateKey != "" {
			privateKey, _ = base64.StdEncoding.DecodeString(creds.PrivateKey)
		}
	} else {
		respondJSON(w, map[string]interface{}{
			"valid": false,
			"error": "access token required",
		})
		return
	}

	if host == "" || user == "" || remotePath == "" {
		respondJSON(w, map[string]interface{}{
			"valid": false,
			"error": "Missing required parameters",
		})
		return
	}

	// Validate remote path - only allow downloads from /home, /opt, and /tmp
	allowedPaths := []string{"/home/", "/opt/", "/tmp/", "/var/log/"}
	isAllowed := false
	for _, prefix := range allowedPaths {
		if strings.HasPrefix(remotePath, prefix) {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		respondJSON(w, map[string]interface{}{
			"valid": false,
			"error": "Access denied: Downloads are only allowed from /home, /opt, /var/log, and /tmp directories",
		})
		return
	}

	// Check if file exists via SSH
	fileInfo, err := validateFileViaSSH(remotePath, host, user, password, privateKey)
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	respondJSON(w, map[string]interface{}{
		"valid":    true,
		"filename": fileInfo["filename"],
		"size":     fileInfo["size"],
	})
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Check if using access token
	accessParam := r.URL.Query().Get("access")
	remotePath := r.URL.Query().Get("path")

	var host, user, password string
	var privateKey []byte
	var err error

	if accessParam != "" {
		// Decrypt access token to get credentials
		creds, err := decryptAccess(accessParam)
		if err != nil {
			http.Error(w, "Invalid access token", http.StatusBadRequest)
			log.Printf("Failed to decrypt access token: %v", err)
			return
		}
		host = creds.Host
		user = creds.User
		password = creds.Password
		if creds.PrivateKey != "" {
			privateKey, _ = base64.StdEncoding.DecodeString(creds.PrivateKey)
		}
	} else {
		http.Error(w, "access token required", http.StatusBadRequest)
		return
	}

	if host == "" || user == "" || remotePath == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// Stream file from SSH server directly to response
	_, err = downloadFileViaSSH(w, remotePath, host, user, password, privateKey)
	if err != nil {
		log.Printf("Download failed: %v", err)
		http.Error(w, "Download failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
	creds.Password = values.Get("password")
	creds.PrivateKey = values.Get("privatekey")

	return creds, nil
}

// getDefaultFernetKey returns the Fernet key from configuration
func getDefaultFernetKey() string {
	return config.Security.FernetKey
}

func encryptAccess(creds SSHCredentials) (string, error) {
	fernetKey := getDefaultFernetKey()
	if fernetKey == "" {
		return "", fmt.Errorf("fernet key not configured")
	}
	keys, err := fernet.DecodeKeys(fernetKey)
	if err != nil {
		return "", fmt.Errorf("invalid fernet key: %v", err)
	}

	values := url.Values{}
	values.Set("username", creds.User)
	values.Set("hostname", creds.Host)
	if creds.Password != "" {
		values.Set("password", creds.Password)
	}
	if creds.PrivateKey != "" {
		values.Set("privatekey", creds.PrivateKey)
	}

	dataB64 := base64.StdEncoding.EncodeToString([]byte(values.Encode()))
	tok, err := fernet.EncryptAndSign([]byte(dataB64), keys[0])
	if err != nil {
		return "", fmt.Errorf("encryption failed: %v", err)
	}
	return string(tok), nil
}

func connectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		respondJSON(w, map[string]interface{}{"error": "Invalid form data"})
		return
	}

	host := r.FormValue("host")
	user := r.FormValue("user")
	password := r.FormValue("password")
	privatekey := r.FormValue("privatekey")

	if host == "" || user == "" {
		respondJSON(w, map[string]interface{}{"error": "host and user are required"})
		return
	}

	token, err := encryptAccess(SSHCredentials{
		Host:       host,
		User:       user,
		Password:   password,
		PrivateKey: privatekey,
	})
	if err != nil {
		log.Printf("Failed to encrypt credentials: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]interface{}{"access": token})
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	accessParam := r.URL.Query().Get("access")
	if accessParam == "" {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: access token required"))
		return
	}

	creds, err := decryptAccess(accessParam)
	if err != nil {
		log.Printf("Failed to decrypt access token: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: Invalid access token"))
		return
	}

	var privateKey []byte
	if creds.PrivateKey != "" {
		privateKey, _ = base64.StdEncoding.DecodeString(creds.PrivateKey)
	}
	handleSSHConnection(conn, creds.Host, creds.User, creds.Password, privateKey)
}
