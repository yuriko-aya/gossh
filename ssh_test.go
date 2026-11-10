package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWSMessageUnmarshal(t *testing.T) {
	raw := `{"type":"input","data":"ls -la","cols":80,"rows":24}`
	var msg WSMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if msg.Type != "input" || msg.Data != "ls -la" || msg.Cols != 80 || msg.Rows != 24 {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestWSMessageS3Pull(t *testing.T) {
	raw := `{"type":"s3_pull","key":"uploads/abc/file.bin","dest":"/tmp/file.bin"}`
	var msg WSMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if msg.Type != "s3_pull" || msg.Key != "uploads/abc/file.bin" || msg.Dest != "/tmp/file.bin" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestParseFileCheckOutput(t *testing.T) {
	path := "/opt/odoo/logs/odoo-server.log.2.gz"

	t.Run("ok", func(t *testing.T) {
		size, err := parseFileCheckOutput("OK 12345\n", path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if size != 12345 {
			t.Fatalf("size = %d, want 12345", size)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		_, err := parseFileCheckOutput("PERMISSION_DENIED", path)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "permission denied") {
			t.Fatalf("error %q should mention permission denied", msg)
		}
		if !strings.Contains(msg, "copy it to /tmp") {
			t.Fatalf("error %q should suggest copying to /tmp", msg)
		}
		if !strings.Contains(msg, path) {
			t.Fatalf("error %q should include the path", msg)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := parseFileCheckOutput("NOT_FOUND", path)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "file not found or not a regular file") {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(err.Error(), "copy it to /tmp") {
			t.Fatalf("not-found error should not suggest copying: %v", err)
		}
	})

	t.Run("not regular", func(t *testing.T) {
		_, err := parseFileCheckOutput("NOT_REGULAR", path)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "file not found or not a regular file") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRateLimiter(t *testing.T) {
	limiter := newRateLimiter(2, time.Minute)

	if !limiter.allow("127.0.0.1") {
		t.Fatal("first request should be allowed")
	}
	if !limiter.allow("127.0.0.1") {
		t.Fatal("second request should be allowed")
	}
	if limiter.allow("127.0.0.1") {
		t.Fatal("third request should be denied")
	}
	if !limiter.allow("127.0.0.2") {
		t.Fatal("different IP should be allowed")
	}
}
