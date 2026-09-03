package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

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
	var err error
	tmpl, err = template.ParseGlob("templates/*.html")
	if err != nil {
		log.Printf("Warning: could not parse templates: %v", err)
	}
}

func main() {
	configPath := os.Getenv("GOSSH_CONFIG")
	if configPath == "" {
		configPath = "config.yaml"
	}
	if err := loadConfig(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	connectLimiter := newRateLimiter(config.RateLimit.ConnectPerMinute, time.Minute)
	wsLimiter := newRateLimiter(config.RateLimit.WSPerMinute, time.Minute)
	presignLimiter := newRateLimiter(config.RateLimit.PresignPerMinute, time.Minute)

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/terminal", terminalHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/presign", withRateLimit(presignLimiter, presignHandler))
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/validate-download", validateDownloadHandler)
	http.HandleFunc("/connect", withRateLimit(connectLimiter, connectHandler))
	http.HandleFunc("/ws", withRateLimit(wsLimiter, wsHandler))
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

	accessParam := r.URL.Query().Get("access")
	if accessParam != "" {
		var err error
		creds, err = decryptAccess(accessParam)
		if err != nil {
			http.Error(w, "Invalid access token", http.StatusBadRequest)
			log.Printf("Failed to decrypt access token: %v", err)
			return
		}

		creds.AccessToken = accessParam

		if tmpl != nil {
			tmpl.ExecuteTemplate(w, "terminal.html", creds)
		} else {
			http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		}
		return
	}

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

	r.ParseMultipartForm(2 << 30)

	file, header, err := r.FormFile("file")
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"success": false,
			"error":   "Failed to read file: " + err.Error(),
		})
		return
	}
	defer file.Close()

	accessParam := r.FormValue("access")
	var host, user, password string
	var privateKey []byte

	if accessParam != "" {
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
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accessParam := r.URL.Query().Get("access")
	if accessParam == "" {
		http.Error(w, "access token required", http.StatusBadRequest)
		return
	}
	if _, err := decryptAccess(accessParam); err != nil {
		http.Error(w, "Invalid access token", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			respondJSON(w, map[string]interface{}{"success": false, "error": "invalid form data"})
			return
		}
	}

	filename := filepath.Base(r.FormValue("filename"))
	if filename == "" || filename == "." {
		filename = filepath.Base(r.URL.Query().Get("filename"))
	}
	if filename == "" || filename == "." {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}

	sizeStr := r.FormValue("size")
	if sizeStr == "" {
		sizeStr = r.URL.Query().Get("size")
	}
	if sizeStr == "" {
		respondJSON(w, map[string]interface{}{"success": false, "error": "missing file size"})
		return
	}
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		respondJSON(w, map[string]interface{}{"success": false, "error": "invalid file size"})
		return
	}
	if err := validateUploadSize(size); err != nil {
		respondJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

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
	accessParam := r.URL.Query().Get("access")
	remotePath := r.URL.Query().Get("path")

	var host, user, password string
	var privateKey []byte
	var err error

	if accessParam != "" {
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

	if !isAllowedDownloadPath(remotePath) {
		respondJSON(w, map[string]interface{}{
			"valid": false,
			"error": allowedDownloadPathError(),
		})
		return
	}

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
	accessParam := r.URL.Query().Get("access")
	remotePath := r.URL.Query().Get("path")

	var host, user, password string
	var privateKey []byte
	var err error

	if accessParam != "" {
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

	_, err = downloadFileViaSSH(w, remotePath, host, user, password, privateKey)
	if err != nil {
		log.Printf("Download failed: %v", err)
		http.Error(w, "Download failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
