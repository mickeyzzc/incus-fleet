package instances

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/unattended"
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
	if err := writeCpioEntry(&cpio, "readme.txt", []byte("source initrd"), 0o100644); err != nil {
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

// fakeToolBin writes an executable shell script; the xorrisoTool wrappers then
// run their real argument building and validation without needing xorriso.
func fakeToolBin(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xorriso")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	return path
}

func TestXorrisoToolAvailable(t *testing.T) {
	if err := newXorrisoTool("definitely-not-a-real-binary-xyz").Available(); err == nil {
		t.Fatal("Available() should fail for a missing binary")
	}
	bin := fakeToolBin(t, "exit 0\n")
	if err := newXorrisoTool(bin).Available(); err != nil {
		t.Fatalf("Available() error = %v", err)
	}
}

func TestXorrisoToolExtract(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "grub.cfg")
	bin := fakeToolBin(t, `cat > "$7" <<'EOF'
menuentry
EOF
exit 0
`)
	content, err := newXorrisoTool(bin).Extract(context.Background(), "/tmp/x.iso", "/boot/grub/grub.cfg", dest)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !strings.Contains(content, "menuentry") {
		t.Fatalf("content = %q", content)
	}
}

func TestXorrisoToolExtractFailsWithoutOutput(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "missing.cfg")
	bin := fakeToolBin(t, "echo SORRY >&2\nexit 1\n")
	_, err := newXorrisoTool(bin).Extract(context.Background(), "/tmp/x.iso", "/boot/grub/grub.cfg", dest)
	if !errors.Is(err, ErrUnattended) || !strings.Contains(err.Error(), "SORRY") {
		t.Fatalf("expected ErrUnattended carrying tool output, got %v", err)
	}
}

func TestXorrisoToolExtractToleratesNonZeroWithOutput(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "grub.cfg")
	bin := fakeToolBin(t, `cat > "$7" <<'EOF'
ok
EOF
echo SORRY >&2
exit 1
`)
	content, err := newXorrisoTool(bin).Extract(context.Background(), "/tmp/x.iso", "/boot/grub/grub.cfg", dest)
	if err != nil {
		t.Fatalf("Extract() should tolerate a non-zero exit with output: %v", err)
	}
	if strings.TrimSpace(content) != "ok" {
		t.Fatalf("content = %q", content)
	}
}

func TestXorrisoToolRebuild(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.iso")
	extra := filepath.Join(dir, "extra.cfg")
	if err := os.WriteFile(extra, []byte("x"), 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	bin := fakeToolBin(t, "dd if=/dev/zero of=\""+dst+"\" bs=1024 count=2048 2>/dev/null\nexit 0\n")
	if err := newXorrisoTool(bin).Rebuild(context.Background(), "/tmp/in.iso", dst,
		map[string]string{"/boot/grub/grub.cfg": "/tmp/patched.cfg"}, extra); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
}

func TestXorrisoToolRebuildRejectsSmallOutput(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out.iso")
	bin := fakeToolBin(t, "printf x > \""+dst+"\"\nexit 0\n")
	err := newXorrisoTool(bin).Rebuild(context.Background(), "/tmp/in.iso", dst, nil, "/tmp/extra")
	if !errors.Is(err, ErrUnattended) || !strings.Contains(err.Error(), "implausibly small") {
		t.Fatalf("expected implausibly-small error, got %v", err)
	}
}

func TestXorrisoToolRebuildCommandFailure(t *testing.T) {
	bin := fakeToolBin(t, "echo xorriso-failed >&2\nexit 2\n")
	err := newXorrisoTool(bin).Rebuild(context.Background(), "/tmp/in.iso",
		filepath.Join(t.TempDir(), "out.iso"), nil, "/tmp/extra")
	if !errors.Is(err, ErrUnattended) {
		t.Fatalf("expected ErrUnattended, got %v", err)
	}
}

func TestXorrisoToolBuildSeed(t *testing.T) {
	dir := t.TempDir()
	bin := fakeToolBin(t, `out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; fi
  shift
done
printf seed > "$out"
exit 0
`)
	files := []unattended.SeedFile{
		{Name: "user-data", Content: "#cloud-config\n"},
		{Name: "meta-data", Content: "instance-id: test\n"},
	}
	path, err := newXorrisoTool(bin).BuildSeed(context.Background(), dir, files)
	if err != nil {
		t.Fatalf("BuildSeed() error = %v", err)
	}
	if path != filepath.Join(dir, "cidata.iso") {
		t.Fatalf("path = %q", path)
	}
	for _, name := range []string{"user-data", "meta-data"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("seed file %s not written: %v", name, err)
		}
	}
}

func TestXorrisoToolBuildSeedFailure(t *testing.T) {
	bin := fakeToolBin(t, "echo boom >&2\nexit 1\n")
	_, err := newXorrisoTool(bin).BuildSeed(context.Background(), t.TempDir(),
		[]unattended.SeedFile{{Name: "user-data", Content: "x"}})
	if !errors.Is(err, ErrUnattended) {
		t.Fatalf("expected ErrUnattended, got %v", err)
	}
}

func TestCombinedTailTruncates(t *testing.T) {
	got := combinedTail([]byte("1\n2\n3\n4\n5\n6"))
	if !strings.Contains(got, "3") || !strings.Contains(got, "6") || strings.Contains(got, "1") {
		t.Fatalf("tail = %q", got)
	}
	if combinedTail(nil) != "" {
		t.Fatalf("empty tail = %q", combinedTail(nil))
	}
}

func TestBootConfigPathsAndDerivedName(t *testing.T) {
	if paths := bootConfigPaths(unattended.ProfileDebianPreseed); len(paths) != 1 || paths[0] != "/boot/grub/grub.cfg" {
		t.Fatalf("preseed paths = %#v", paths)
	}
	if paths := bootConfigPaths(unattended.ProfileKickstart); len(paths) != 3 {
		t.Fatalf("kickstart paths = %#v", paths)
	}
	if paths := bootConfigPaths(unattended.Profile("unknown")); paths != nil {
		t.Fatalf("unknown paths = %#v", paths)
	}
	if name := derivedISOName("debian", unattended.ProfileDebianPreseed, "vm1"); name != "debian-debian-preseed-auto-vm1" {
		t.Fatalf("derived name = %q", name)
	}
}

// TestRepackInitrdRejectsNonGzip covers the validation that keeps a malformed
// initrd from producing a silently unbootable installer.
func TestDerivedISONameTruncatesLongNames(t *testing.T) {
	longISO := strings.Repeat("very-long-installer-name-", 4)
	got := derivedISOName(longISO, unattended.ProfileDebianPreseed, "vm-with-long-name")
	if len(got) > 63 {
		t.Fatalf("derivedISOName() length = %d, want <= 63", len(got))
	}
	if len(got) < 9 || strings.Trim(got[len(got)-8:], "0123456789abcdef") != "" {
		t.Fatalf("derivedISOName() = %q, want a hash suffix", got)
	}
	if got == longISO {
		t.Fatal("derivedISOName() should shorten long names")
	}
}

func TestRepackInitrdRejectsNonGzip(t *testing.T) {
	if _, err := repackInitrd([]byte("not gzip at all"), "preseed.cfg", []byte("x")); !errors.Is(err, ErrUnattended) {
		t.Fatalf("repackInitrd() = %v, want ErrUnattended", err)
	}
}
