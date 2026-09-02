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
