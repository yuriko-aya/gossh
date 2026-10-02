package main

import "testing"

func TestIsAllowedDownloadPath(t *testing.T) {
	tests := []struct {
		path  string
		allow bool
	}{
		{"/home/user/file.txt", true},
		{"/opt/app/config.yaml", true},
		{"/tmp/upload.bin", true},
		{"/var/log/syslog", true},
		{"/var/tmp/upload.bin", true},
		{"/etc/passwd", false},
		{"/root/.ssh/id_rsa", false},
		{"../tmp/evil", false},
	}

	for _, tc := range tests {
		got := isAllowedDownloadPath(tc.path)
		if got != tc.allow {
			t.Errorf("isAllowedDownloadPath(%q) = %v, want %v", tc.path, got, tc.allow)
		}
	}
}

func TestIsAllowedUploadPath(t *testing.T) {
	tests := []struct {
		path  string
		allow bool
	}{
		{"/tmp/upload.bin", true},
		{"/var/tmp/largefile.bin", true},
		{"/tmp/../etc/passwd", false},
		{"/home/user/file.txt", false},
		{"/var/log/syslog", false},
	}

	for _, tc := range tests {
		got := isAllowedUploadPath(tc.path)
		if got != tc.allow {
			t.Errorf("isAllowedUploadPath(%q) = %v, want %v", tc.path, got, tc.allow)
		}
	}
}
