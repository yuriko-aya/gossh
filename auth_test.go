package main

import (
	"testing"
	"time"
)

func setupTestConfig(t *testing.T) {
	t.Helper()
	config.Security.FernetKey = "cw_0x689RpI-jtRR7oE8h_eQsKImvJapLeSbXpwF4e4="
	config.Security.TokenTTL = "24h"
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	setupTestConfig(t)

	creds := SSHCredentials{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	token, err := encryptAccess(creds)
	if err != nil {
		t.Fatalf("encryptAccess failed: %v", err)
	}

	got, err := decryptAccess(token)
	if err != nil {
		t.Fatalf("decryptAccess failed: %v", err)
	}

	if got.Host != creds.Host || got.User != creds.User || got.Password != creds.Password {
		t.Fatalf("credentials mismatch: got %+v, want %+v", got, creds)
	}
}

func TestDecryptExpiredToken(t *testing.T) {
	setupTestConfig(t)

	token, err := encryptAccess(SSHCredentials{Host: "example.com", User: "admin"})
	if err != nil {
		t.Fatalf("encryptAccess failed: %v", err)
	}

	config.Security.TokenTTL = "1ms"
	time.Sleep(5 * time.Millisecond)

	if _, err := decryptAccess(token); err == nil {
		t.Fatal("expected expired token to fail decryption")
	}
}

func TestDecryptInvalidToken(t *testing.T) {
	setupTestConfig(t)

	if _, err := decryptAccess("not-a-valid-token"); err == nil {
		t.Fatal("expected invalid token to fail")
	}
}
