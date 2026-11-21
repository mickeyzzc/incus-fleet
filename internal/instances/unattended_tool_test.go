package instances

import (
	"bytes"
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
func TestRepackInitrdAddsSeed(t *testing.T) {
	content := "d-i debian-installer/locale string en_US"

	// A tiny valid source initrd: one gzip'd cpio with one file.
	var src bytes.Buffer
	zw := gzip.NewWriter(&src)
	var cpio bytes.Buffer
	if err := writeCpioEntry(&cpio, "readme.txt", []byte("source initrd")); err != nil {
		t.Fatal(err)
	}
	if err := writeCpioTrailer(&cpio); err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write(cpio.Bytes())
	_ = zw.Close()

	segment, err := repackInitrd(src.Bytes(), "preseed.cfg", []byte(content))
	if err != nil {
		t.Fatalf("repackInitrd() error = %v", err)
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

	// First entry is the carried-over source file; its name follows the
	// 110-byte header.
	if string(plain[110:120]) != "readme.txt" {
		t.Fatalf("first entry name = %q", plain[110:120])
	}

	// The data is padded to 4 bytes; the segment ends with the trailer.
	if !strings.Contains(string(plain), content) {
		t.Error("content missing from segment")
	}
	if !strings.Contains(string(plain), "readme.txt") {
		t.Error("original entry lost in repack")
	}
	if strings.Count(string(plain), "TRAILER!!!") != 1 {
		t.Errorf("expected exactly one trailer, got %d", strings.Count(string(plain), "TRAILER!!!"))
	}

	// Total length must be a multiple of 4 (padding rule of newc).
	if len(plain)%4 != 0 {
		t.Fatalf("segment length %d is not 4-byte aligned", len(plain))
	}
}
