package main

import "testing"

func TestContainsPort(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"example.com", false},
		{"example.com:22", true},
		{"192.168.1.1:2222", true},
		{"[::1]", false},
		{"[::1]:22", true},
	}

	for _, tc := range tests {
		if got := containsPort(tc.host); got != tc.want {
			t.Errorf("containsPort(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestNormalizeHostAddr(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"example.com", "example.com:22"},
		{"example.com:2222", "example.com:2222"},
		{"10.0.0.1", "10.0.0.1:22"},
	}

	for _, tc := range tests {
		if got := normalizeHostAddr(tc.host); got != tc.want {
			t.Errorf("normalizeHostAddr(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestBuildSSHConfig(t *testing.T) {
	cfg, err := buildSSHConfig("user", "pass", nil)
	if err != nil {
		t.Fatalf("buildSSHConfig failed: %v", err)
	}
	if cfg.User != "user" {
		t.Fatalf("expected user 'user', got %q", cfg.User)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(cfg.Auth))
	}
}
