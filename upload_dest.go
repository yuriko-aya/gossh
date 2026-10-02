package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

const uploadDestScript = `tmp_src=$(findmnt -no SOURCE -- /tmp 2>/dev/null || df --output=source /tmp | tail -1)
root_src=$(findmnt -no SOURCE -- / 2>/dev/null || df --output=source / | tail -1)
if [ "$tmp_src" = "$root_src" ]; then base=/tmp; else base=/var/tmp; fi
avail=$(df -B1 --output=avail "$base" | tail -1)
printf '%s %s\n' "$base" "$avail"`

func parseUploadDestOutput(output string) (base string, avail int64, err error) {
	line := strings.TrimSpace(output)
	if line == "" {
		return "", 0, fmt.Errorf("empty upload destination check output")
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, fmt.Errorf("unexpected upload destination check output: %q", output)
	}

	base = fields[0]
	avail, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("parse available space: %w", err)
	}

	if base != "/tmp" && base != "/var/tmp" {
		return "", 0, fmt.Errorf("unexpected upload base path: %q", base)
	}

	return base, avail, nil
}

func formatByteSize(n int64) string {
	const gb = 1024 * 1024 * 1024
	if n >= gb {
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	}
	const mb = 1024 * 1024
	return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
}

func resolveUploadDestination(sshConn *ssh.Client, filename string, fileSize int64) (destPath string, err error) {
	session, err := sshConn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create upload destination session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(uploadDestScript)
	if err != nil {
		return "", fmt.Errorf("upload destination check failed: %w", err)
	}

	base, avail, err := parseUploadDestOutput(string(output))
	if err != nil {
		return "", err
	}

	if avail < fileSize {
		return "", fmt.Errorf(
			"insufficient space on %s: need %s, avail %s",
			base, formatByteSize(fileSize), formatByteSize(avail),
		)
	}

	safeName := filepath.Base(filename)
	if safeName == "" || safeName == "." {
		return "", fmt.Errorf("invalid filename")
	}

	return filepath.Join(base, safeName), nil
}
