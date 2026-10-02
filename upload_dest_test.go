package main

import "testing"

const mountinfoTmpOnRoot = `25 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
26 25 259:2 / /var rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
`

const mountinfoTmpfs = `60 28 0:43 / /tmp rw,nosuid,nodev shared:93 - tmpfs tmpfs rw,size=465240k,nr_inodes=1048576,inode64,usrquota
25 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
`

func TestParseMountInfoLine(t *testing.T) {
	entry, ok := parseMountInfoLine("60 28 0:43 / /tmp rw,nosuid,nodev shared:93 - tmpfs tmpfs rw,size=465240k")
	if !ok {
		t.Fatal("expected parse success")
	}
	if entry.mountPoint != "/tmp" {
		t.Fatalf("mountPoint = %q, want /tmp", entry.mountPoint)
	}
}

func TestHasTmpMountPoint(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"no separate /tmp mount", mountinfoTmpOnRoot, false},
		{"tmpfs /tmp mount", mountinfoTmpfs, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseMountInfo(tc.data)
			if err != nil {
				t.Fatalf("parseMountInfo: %v", err)
			}
			got := hasTmpMountPoint(entries)
			if got != tc.want {
				t.Fatalf("hasTmpMountPoint() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChooseUploadBaseFromMounts(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantBase string
	}{
		{"tmp on root filesystem", mountinfoTmpOnRoot, "/tmp"},
		{"tmpfs /tmp", mountinfoTmpfs, "/var/tmp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseMountInfo(tc.data)
			if err != nil {
				t.Fatalf("parseMountInfo: %v", err)
			}
			got := chooseUploadBaseFromMounts(entries)
			if got != tc.wantBase {
				t.Fatalf("chooseUploadBaseFromMounts() = %q, want %q", got, tc.wantBase)
			}
		})
	}
}

func TestParseDFAvail(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int64
		wantErr bool
	}{
		{
			name:   "typical df",
			output: "     Avail\n5368709120\n",
			want:   5368709120,
		},
		{
			name:    "empty",
			output:  "",
			wantErr: true,
		},
		{
			name:    "malformed",
			output:  "Avail\nnot-a-number\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDFAvail(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseDFAvail() = %d, want %d", got, tc.want)
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
