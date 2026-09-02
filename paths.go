package main

import "strings"

var allowedDownloadPaths = []string{"/home/", "/opt/", "/tmp/", "/var/log/"}

func isAllowedDownloadPath(remotePath string) bool {
	for _, prefix := range allowedDownloadPaths {
		if strings.HasPrefix(remotePath, prefix) {
			return true
		}
	}
	return false
}

func allowedDownloadPathError() string {
	return "Access denied: Downloads are only allowed from /home, /opt, /var/log, and /tmp directories"
}
