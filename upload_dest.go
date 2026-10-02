package main

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

const remoteMountInfoPath = "/proc/self/mountinfo"

type mountInfoEntry struct {
	mountPoint string
}

func unescapeMountPoint(s string) string {
	s = strings.ReplaceAll(s, "\\134", `\`)
	s = strings.ReplaceAll(s, "\\040", " ")
	s = strings.ReplaceAll(s, "\\011", "\t")
	s = strings.ReplaceAll(s, "\\012", "\n")
	s = strings.ReplaceAll(s, "\\042", `"`)
	return s
}

func parseMountInfo(data string) ([]mountInfoEntry, error) {
	var entries []mountInfoEntry
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		entry, ok := parseMountInfoLine(scanner.Text())
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no mount entries parsed from mountinfo")
	}
	return entries, nil
}

func parseMountInfoLine(line string) (mountInfoEntry, bool) {
	sepIdx := strings.Index(line, " - ")
	if sepIdx < 0 {
		return mountInfoEntry{}, false
	}

	lhs := strings.Fields(line[:sepIdx])
	if len(lhs) < 5 {
		return mountInfoEntry{}, false
	}

	return mountInfoEntry{
		mountPoint: filepath.Clean(unescapeMountPoint(lhs[4])),
	}, true
}

func hasTmpMountPoint(entries []mountInfoEntry) bool {
	for _, entry := range entries {
		if entry.mountPoint == "/tmp" {
			return true
		}
	}
	return false
}

func chooseUploadBaseFromMounts(entries []mountInfoEntry) string {
	if hasTmpMountPoint(entries) {
		return "/var/tmp"
	}
	return "/tmp"
}

func parseDFAvail(output string) (int64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.EqualFold(line, "Avail") {
			continue
		}
		avail, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse available space: %w", err)
		}
		return avail, nil
	}
	return 0, fmt.Errorf("unexpected df output: %q", output)
}

func formatByteSize(n int64) string {
	const gb = 1024 * 1024 * 1024
	if n >= gb {
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	}
	const mb = 1024 * 1024
	return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
}

func readRemoteFile(sshConn *ssh.Client, path string) ([]byte, error) {
	session, err := sshConn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("cat " + path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return output, nil
}

func remoteAvailableBytes(sshConn *ssh.Client, path string) (int64, error) {
	session, err := sshConn.NewSession()
	if err != nil {
		return 0, fmt.Errorf("failed to create df session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("df -B1 --output=avail " + path)
	if err != nil {
		return 0, fmt.Errorf("df on %s: %w", path, err)
	}
	return parseDFAvail(string(output))
}

func resolveUploadDestination(sshConn *ssh.Client, filename string, fileSize int64) (destPath string, err error) {
	mountData, err := readRemoteFile(sshConn, remoteMountInfoPath)
	if err != nil {
		return "", fmt.Errorf("upload destination check failed: %w", err)
	}

	entries, err := parseMountInfo(string(mountData))
	if err != nil {
		return "", fmt.Errorf("upload destination check failed: %w", err)
	}

	base := chooseUploadBaseFromMounts(entries)

	avail, err := remoteAvailableBytes(sshConn, base)
	if err != nil {
		return "", fmt.Errorf("upload destination check failed: %w", err)
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
