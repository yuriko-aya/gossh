package main

import (
	"encoding/json"
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
