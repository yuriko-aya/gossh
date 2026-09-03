package main

import "testing"

func TestValidateUploadSize(t *testing.T) {
	if err := validateUploadSize(1024); err != nil {
		t.Fatalf("expected 1KB to be allowed: %v", err)
	}
	if err := validateUploadSize(MaxS3PutObjectSize); err != nil {
		t.Fatalf("expected 5GB to be allowed: %v", err)
	}
	if err := validateUploadSize(MaxS3PutObjectSize + 1); err == nil {
		t.Fatal("expected size over 5GB to be rejected")
	}
	if err := validateUploadSize(0); err == nil {
		t.Fatal("expected zero size to be rejected")
	}
}
