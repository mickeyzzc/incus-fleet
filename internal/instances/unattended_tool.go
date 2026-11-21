package instances

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
// one extra file at the ISO root.
func (t *xorrisoTool) Rebuild(ctx context.Context, srcPath, dstPath string, patched map[string]string, extraPath string) error {
	args := []string{"-indev", srcPath, "-outdev", dstPath}
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
