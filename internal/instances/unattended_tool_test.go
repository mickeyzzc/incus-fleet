package instances

import (
	"bytes"
	"fmt"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

// TestAppendInitrdSeedLayout validates the cpio newc byte layout of the
// initramfs seed segment: a 110-byte header (6-byte magic + 13 8-hex
// fields), the name padded to 4 bytes, the data padded to 4 bytes, and the
// trailer. A malformed segment is silently skipped by the kernel's
// initramfs unpacker, which is exactly the failure this guards against.
func TestAppendInitrdSeedLayout(t *testing.T) {
	content := "d-i debian-installer/locale string en_US"

	segment, err := appendInitrdSeed("preseed.cfg", content)
	if err != nil {
		t.Fatalf("appendInitrdSeed() error = %v", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(segment))
	if err != nil {
		t.Fatalf("segment is not gzip: %v", err)
	}
	plain, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gunzip failed: %v", err)
	}

	// Header: 6-byte magic then 13 8-hex fields.
	if string(plain[:6]) != "070701" {
		t.Fatalf("magic = %q", plain[:6])
	}
	fields := string(plain[6:110])
	for i, c := range fields {
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			t.Fatalf("field byte %d = %q, want uppercase hex", i, c)
		}
	}

	// Filesize field (7th field) holds len(content).
	sizeField := string(plain[6+6*8 : 6+7*8])
	var parsedSize int
	if _, err := fmt.Sscanf(sizeField, "%8X", &parsedSize); err != nil || parsedSize != len(content) {
		t.Fatalf("filesize field = %q, want %08X", sizeField, len(content))
	}

	// Name starts right after the 110-byte header.
	if string(plain[110:121]) != "preseed.cfg" {
		t.Fatalf("name = %q", plain[110:121])
	}

	// The data is padded to 4 bytes; the segment ends with the trailer.
	if !strings.Contains(string(plain), content) {
		t.Error("content missing from segment")
	}
	if !strings.Contains(string(plain), "TRAILER!!!") {
		t.Error("trailer missing from segment")
	}

	// Total length must be a multiple of 4 (padding rule of newc).
	if len(plain)%4 != 0 {
		t.Fatalf("segment length %d is not 4-byte aligned", len(plain))
	}
}
