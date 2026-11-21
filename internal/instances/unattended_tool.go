package instances

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/unattended"
)

// isoTool performs the on-disk ISO surgery for unattended installs: pulling
// boot configurations out of an image, rebuilding patched images and
// building NoCloud seed media. Production uses xorriso; tests inject a stub.
type isoTool interface {
	// Available reports whether the tool can run on this host.
	Available() error
	// Extract copies one file out of an ISO image and returns its content.
	Extract(ctx context.Context, isoPath, isoFilePath, destPath string) (string, error)
	// Rebuild clones src into dst while updating patched boot
	// configurations and adding one extra file at the ISO root.
	Rebuild(ctx context.Context, srcPath, dstPath string, patched map[string]string, extraPath string) error
	// BuildSeed writes the seed files into dir and builds a NoCloud seed
	// ISO (volume label cidata); it returns the built file path.
	BuildSeed(ctx context.Context, dir string, files []unattended.SeedFile) (string, error)
}

// xorrisoTool is the production isoTool backed by the xorriso binary.
type xorrisoTool struct {
	bin string
}

func newXorrisoTool(bin string) *xorrisoTool {
	return &xorrisoTool{bin: bin}
}

// Available checks that xorriso resolves on the host.
func (t *xorrisoTool) Available() error {
	if _, err := exec.LookPath(t.bin); err != nil {
		return fmt.Errorf("xorriso (%q) is required on the fleet server for unattended ISO installs: %w", t.bin, err)
	}

	return nil
}

// Extract pulls one file out of an ISO image via xorriso osirrox.
// Rebuilt images can carry an isohybrid MBR whose partition table exceeds
// the cloned file size; xorriso then exits with a tolerated SORRY even
// though the extraction succeeded. The extracted file is the authority.
func (t *xorrisoTool) Extract(ctx context.Context, isoPath, isoFilePath, destPath string) (string, error) {
	cmd := exec.CommandContext(ctx, t.bin, "-osirrox", "on", "-indev", isoPath, "-extract", isoFilePath, destPath) //nolint:gosec // tool path is configured, paths are inside the controlled work dir
	out, cmdErr := cmd.CombinedOutput()

	info, statErr := os.Stat(destPath)
	if cmdErr != nil && (statErr != nil || info == nil || info.Size() == 0) {
		return "", fmt.Errorf("%w: extract %s from iso: %s", ErrUnattended, isoFilePath, combinedTail(out))
	}

	content, err := os.ReadFile(destPath) //nolint:gosec // path built inside the controlled work dir
	if err != nil {
		return "", fmt.Errorf("%w: read extracted %s: %w", ErrUnattended, isoFilePath, err)
	}

	return string(content), nil
}

// Rebuild clones src into dst while updating boot configurations and adding
// one extra file at the ISO root. -boot_image any replay re-creates the
// boot catalog (El Torito BIOS+EFI entries) from the loaded image: a plain
// clone of Debian's isohybrid images loses the catalog and the firmware
// then reports "UEFI QEMU QEMU CD-ROM ... Not Found".
func (t *xorrisoTool) Rebuild(ctx context.Context, srcPath, dstPath string, patched map[string]string, extraPath string) error {
	args := []string{"-indev", srcPath, "-outdev", dstPath, "-boot_image", "any", "replay"}
	for isoPath, localPath := range patched {
		args = append(args, "-update", localPath, isoPath)
	}
	args = append(args, "-add", extraPath, "--")

	cmd := exec.CommandContext(ctx, t.bin, args...) //nolint:gosec // see above
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: rebuild iso: %s", ErrUnattended, combinedTail(out))
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		return fmt.Errorf("%w: rebuilt iso missing: %w", ErrUnattended, err)
	}
	if info.Size() < 1<<20 {
		return fmt.Errorf("%w: rebuilt iso is implausibly small (%d bytes)", ErrUnattended, info.Size())
	}

	return nil
}

// BuildSeed renders a NoCloud seed ISO (volume label cidata) with the given
// files via xorriso's mkisofs emulation.
func (t *xorrisoTool) BuildSeed(ctx context.Context, dir string, files []unattended.SeedFile) (string, error) {
	var inputs []string
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if err := os.WriteFile(path, []byte(file.Content), 0o600); err != nil {
			return "", fmt.Errorf("%w: write seed file %s: %w", ErrUnattended, file.Name, err)
		}
		inputs = append(inputs, path)
	}

	outPath := filepath.Join(dir, "cidata.iso")
	args := append([]string{"-as", "mkisofs", "-V", "cidata", "-J", "-r", "-o", outPath}, inputs...) //nolint:gocritic // xorriso input list
	cmd := exec.CommandContext(ctx, t.bin, args...)                                                  //nolint:gosec // see above
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: build seed iso: %s", ErrUnattended, combinedTail(out))
	}

	return outPath, nil
}

// combinedTail keeps the last lines of command output for error wrapping.
func combinedTail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}

	return strings.Join(lines, " | ")
}

// bootConfigPaths lists the GRUB configuration locations per installer
// family, most specific first.
func bootConfigPaths(profile unattended.Profile) []string {
	switch profile {
	case unattended.ProfileDebianPreseed:
		return []string{"/boot/grub/grub.cfg"}
	case unattended.ProfileKickstart:
		return []string{"/EFI/BOOT/grub.cfg", "/boot/grub2/grub.cfg", "/boot/grub/grub.cfg"}
	default:
		return nil
	}
}

// derivedISOName builds the derived installer volume name for one VM.
func derivedISOName(isoName string, profile unattended.Profile) string {
	return fmt.Sprintf("%s-%s-auto", isoName, profile)
}

// volumeNameRE constrains the names of derived installation media volumes.
var volumeNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// repackInitrd adds the answer file at the initramfs root of a gzip'd cpio
// initrd. It fully decompresses the original archive, re-emits every entry
// plus the seed file, and re-compresses: a single-member gzip cpio is the
// most kernel-compatible shape (appended segments - compressed or not -
// were not unpacked by the d-i guest kernel in practice).
func repackInitrd(initrd []byte, answerName string, answer []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(initrd))
	if err != nil {
		return nil, fmt.Errorf("%w: initrd is not gzip: %w", ErrUnattended, err)
	}
	plain, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("%w: initrd decompress failed: %w", ErrUnattended, err)
	}

	var out bytes.Buffer
	if err := writeCpioEntries(&out, plain); err != nil {
		return nil, err
	}
	if err := writeCpioEntry(&out, answerName, answer, 0o100644); err != nil {
		return nil, err
	}
	if err := writeCpioTrailer(&out); err != nil {
		return nil, err
	}

	var compressed bytes.Buffer
	gzOut := gzip.NewWriter(&compressed)
	if _, err := gzOut.Write(out.Bytes()); err != nil {
		return nil, err
	}
	if err := gzOut.Close(); err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

// cpioHeaderLen is the size of a newc header: 6-byte magic + 13 fields.
const cpioHeaderLen = 110

// cpioMagic is the newc magic.
const cpioMagic = "070701"

// parseHexField reads one 8-hex-digit field from a newc header block.
func parseHexField(header []byte, index int) (int64, error) {
	field := string(header[index*8 : (index+1)*8])
	value, err := strconv.ParseInt(field, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: malformed cpio field %d (%q): %w", ErrUnattended, index, field, err)
	}

	return value, nil
}

// writeCpioEntries re-emits every entry of a single newc archive stream
// into out.
func writeCpioEntries(out *bytes.Buffer, plain []byte) error {
	pos := 0
	for {
		if pos+cpioHeaderLen > len(plain) {
			return fmt.Errorf("%w: initrd cpio truncated (header at %d)", ErrUnattended, pos)
		}

		header := plain[pos : pos+cpioHeaderLen]
		if string(header[:6]) != cpioMagic {
			return fmt.Errorf("%w: initrd cpio entry at %d has magic %q", ErrUnattended, pos, header[:6])
		}

		fileSize, err := parseHexField(header[6:], 6) // c_filesize is field 6
		if err != nil {
			return err
		}
		mode, modeErr := parseHexField(header[6:], 1) // c_mode is field 1
		if modeErr != nil {
			return modeErr
		}
		nameSize, err := parseHexField(header[6:], 11) // c_namesize is field 11
		if err != nil {
			return err
		}

		nameStart := pos + cpioHeaderLen
		nameEnd := nameStart + int(nameSize) // includes the trailing NUL
		if nameEnd > len(plain) {
			return fmt.Errorf("%w: initrd cpio name truncated", ErrUnattended)
		}

		name := string(plain[nameStart : nameEnd-1])
		if name == "TRAILER!!!" {
			return nil
		}

		dataStart := align4(nameEnd)
		dataEnd := dataStart + int(fileSize)
		if dataEnd > len(plain) {
			return fmt.Errorf("%w: initrd cpio data truncated for %s", ErrUnattended, name)
		}

		if err := writeCpioEntry(out, name, plain[dataStart:dataEnd], mode); err != nil {
			return err
		}

		pos = align4(dataEnd)
	}
}

// align4 rounds n up to the next multiple of 4.
func align4(n int) int {
	return n + (4-n%4)%4
}

// writeCpioEntry writes one file in the cpio "newc" format: a 6-byte magic
// followed by 13 8-hex-digit fields (110 bytes), the name padded to 4 bytes
// and the data padded to 4 bytes. mode is caller-supplied so repacking
// preserves the original initramfs metadata (losing the execute bit on
// /init bricks the boot with error -13).
func writeCpioEntry(w *bytes.Buffer, name string, data []byte, mode int64) error {
	if strings.Contains(name, "\x00") || name == "" {
		return fmt.Errorf("%w: invalid cpio entry name", ErrUnattended)
	}

	fields := fmt.Sprintf("%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X",
		1,    // ino
		mode,
		0,    // uid
		0,    // gid
		1,    // nlink
		0,    // mtime
		len(data),
		0, // devmajor
		0, // devminor
		0, // rdevmajor
		0, // rdevminor
		len(name)+1,
		0, // check
	)

	w.WriteString("070701")
	w.WriteString(fields)
	w.WriteString(name)
	w.WriteByte(0)

	if pad := (4 - (110+len(name)+1)%4) % 4; pad > 0 {
		w.Write(make([]byte, pad))
	}
	w.Write(data)
	if pad := (4 - len(data)%4) % 4; pad > 0 {
		w.Write(make([]byte, pad))
	}

	return nil
}

// writeCpioTrailer ends a cpio archive.
func writeCpioTrailer(w *bytes.Buffer) error {
	name := "TRAILER!!!"

	fields := fmt.Sprintf("%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X",
		0, 0, 0, 0,
		1, // nlink
		0, 0, 0, 0, 0, 0,
		len(name)+1,
		0,
	)

	w.WriteString("070701")
	w.WriteString(fields)
	w.WriteString(name)
	w.WriteByte(0)

	if pad := (4 - (110+len(name)+1)%4) % 4; pad > 0 {
		w.Write(make([]byte, pad))
	}
	if pad := (4 - w.Len()%4) % 4; pad > 0 {
		w.Write(make([]byte, pad))
	}

	return nil
}
