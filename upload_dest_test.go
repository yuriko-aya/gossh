package main

import "testing"

func TestParseUploadDestOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantBase  string
		wantAvail int64
		wantErr   bool
	}{
		{
			name:      "tmp on root",
			output:    "/tmp 5368709120\n",
			wantBase:  "/tmp",
			wantAvail: 5368709120,
		},
		{
			name:      "var tmp separate",
			output:    "/var/tmp 21474836480\n",
			wantBase:  "/var/tmp",
			wantAvail: 21474836480,
		},
		{
			name:    "empty",
			output:  "",
			wantErr: true,
		},
		{
			name:    "unexpected base",
			output:  "/home 1000\n",
			wantErr: true,
		},
		{
			name:    "malformed avail",
			output:  "/tmp not-a-number\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, avail, err := parseUploadDestOutput(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if base != tc.wantBase || avail != tc.wantAvail {
				t.Fatalf("parseUploadDestOutput(%q) = (%q, %d), want (%q, %d)", tc.output, base, avail, tc.wantBase, tc.wantAvail)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	if got := formatByteSize(3 * 1024 * 1024 * 1024); got != "3.0 GB" {
		t.Fatalf("formatByteSize(3GB) = %q, want 3.0 GB", got)
	}
	if got := formatByteSize(512 * 1024 * 1024); got != "512.0 MB" {
		t.Fatalf("formatByteSize(512MB) = %q, want 512.0 MB", got)
	}
}
