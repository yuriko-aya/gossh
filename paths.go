package main

import (
	"path/filepath"
	"strings"
)

var allowedDownloadPaths = []string{"/home/", "/opt/", "/tmp/", "/var/log/", "/var/tmp/"}

var allowedUploadPathPrefixes = []string{"/tmp/", "/var/tmp/"}

func isAllowedDownloadPath(remotePath string) bool {
	for _, prefix := range allowedDownloadPaths {
		if strings.HasPrefix(remotePath, prefix) {
			return true
		}
	}
	return false
}

func isAllowedUploadPath(path string) bool {
	cleaned := filepath.Clean(path)
	for _, prefix := range allowedUploadPathPrefixes {
		if strings.HasPrefix(cleaned, prefix) {
			return true
		}
	}
	return false
}

func allowedDownloadPathError() string {
	return "Access denied: Downloads are only allowed from /home, /opt, /var/log, /tmp, and /var/tmp directories"
}
