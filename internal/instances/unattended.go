package instances

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/unattended"
)

// Unattended install timeouts and watch intervals. Package variables so
// tests can shorten the watch cycle.
var (
	unattendedLaunchTimeout = 40 * time.Minute
	unattendedWatchTimeout  = 6 * time.Hour
	unattendedPollInterval  = 15 * time.Second
	// minBootAge guards against racing an early installer stop: the media
	// watcher only fires after the installer actually ran.
	minBootAge = 60 * time.Second
)

// UnattendedRequest launches a VM that installs itself from an existing ISO
// without keyboard input.
type UnattendedRequest struct {
	Name string `json:"name"`
	// Profile selects the installer family (debian-preseed,
	// ubuntu-autoinstall, kickstart, nocloud).
	Profile string `json:"profile"`
	// AnswerFile overrides the built-in template with operator-provided
	// content (preseed.cfg / ks.cfg / user-data).
	AnswerFile string                `json:"answer_file,omitempty"`
	Spec       unattended.AnswerSpec `json:"spec,omitempty"`
	Network    string                `json:"network,omitempty"`
	RootDisk   string                `json:"root_disk,omitempty"`
	Start      *bool                 `json:"start,omitempty"`
}

// UnattendedLaunch reports the artifacts and watch state of one unattended
// launch.
type UnattendedLaunch struct {
	Name string `json:"name"`
	// Media are the volumes created for the install (derived installer
	// ISO or NoCloud seed ISO). They are detached and deleted
	// automatically once the installer finishes.
	Media []UnattendedMedia `json:"media"`
	// Profile is the normalized unattended profile.
	Profile unattended.Profile `json:"profile"`
	// AnswerFileName is the on-media file name of the answer file.
	AnswerFileName string `json:"answer_file_name"`
}

// UnattendedMedia describes one installation media volume created for the
// unattended install.
type UnattendedMedia struct {
	Device string `json:"device"`
	Volume string `json:"volume"`
	Pool   string `json:"pool"`
	// Seed is true for the NoCloud seed media; the unmodified installer
	// ISO stays attached as iso0 in that case.
	Seed bool `json:"seed"`
}

// ErrUnattended marks unattended-install failures. Errors that also wrap
// instances.ErrInvalidRequest surface as 400s; the rest are server-side
// (missing xorriso, storage failures) and surface as 500s.
var ErrUnattended = errors.New("instances: unattended install")

// LaunchUnattended creates a VM that installs itself from an ISO without
// keyboard input.
//
// Two artifact paths exist:
//   - preseed/kickstart profiles remaster the installer ISO: the answer file
//     is added at the ISO root and the GRUB boot configuration is patched to
//     carry the unattended kernel arguments (auto=true priority=critical
//     file=/cdrom/preseed.cfg or inst.ks=cdrom:/ks.cfg).
//   - autoinstall/nocloud profiles keep the installer ISO untouched and
//     attach a generated NoCloud seed ISO (volume label cidata) with
//     user-data/meta-data.
//
// The installation media is watched and detached (plus the created volumes
// deleted) once the installer finishes; see watchUnattendedInstall.
func (s *Service) LaunchUnattended(ctx context.Context, clusterName, pool, isoName string, req UnattendedRequest) (Detail, UnattendedLaunch, error) {
	ctx, cancel := context.WithTimeout(ctx, unattendedLaunchTimeout)
	defer cancel()

	profile := unattended.Profile(strings.TrimSpace(req.Profile))
	if err := unattended.Validate(profile); err != nil {
		return Detail{}, UnattendedLaunch{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return Detail{}, UnattendedLaunch{}, fmt.Errorf("%w: instance name is required", ErrInvalidRequest)
	}
	if !volumeNameRE.MatchString(req.Name) {
		return Detail{}, UnattendedLaunch{}, fmt.Errorf("%w: invalid instance name %q", ErrInvalidRequest, req.Name)
	}

	pool = strings.TrimSpace(pool)
	if pool == "" {
		pool = "default"
	}

	answer, err := s.renderUnattendedAnswer(profile, req)
	if err != nil {
		return Detail{}, UnattendedLaunch{}, err
	}

	workDir, err := s.unattendedWorkDir()
	if err != nil {
		return Detail{}, UnattendedLaunch{}, err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, UnattendedLaunch{}, err
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return Detail{}, UnattendedLaunch{}, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}
	if !containsString(pools, pool) {
		return Detail{}, UnattendedLaunch{}, fmt.Errorf("%w: storage pool %q does not exist on %s", ErrInvalidRequest, pool, clusterName)
	}

	// In clustered Incus, custom ISO volumes live on one member. Derive the
	// volume there and create the VM on the same member so device
	// validation can actually load the media.
	mediaMember, err := s.isoVolumeMember(client, pool, isoName)
	if err != nil {
		return Detail{}, UnattendedLaunch{}, err
	}

	media := []UnattendedMedia{}
	cleanup := func() {
		s.cleanupUnattendedMedia(client, pool, media)
	}

	installerISO := isoName
	if unattended.NeedsRemaster(profile) {
		derived, err := s.remasterInstallerISO(ctx, clusterName, mediaMember, pool, isoName, profile, answer, workDir)
		if err != nil {
			return Detail{}, UnattendedLaunch{}, err
		}
		installerISO = derived
		media = append(media, UnattendedMedia{Device: "iso0", Volume: derived, Pool: pool})
	}

	var seedISO string
	seedFiles, err := unattended.RenderSeedFiles(profile, req.Spec)
	if err != nil {
		cleanup()
		return Detail{}, UnattendedLaunch{}, err
	}
	if len(seedFiles) > 0 {
		volumeName, err := s.buildAndImportSeedISO(ctx, clusterName, mediaMember, pool, req.Name, seedFiles, workDir)
		if err != nil {
			cleanup()
			return Detail{}, UnattendedLaunch{}, err
		}
		seedISO = volumeName
		media = append(media, UnattendedMedia{Device: "iso1", Volume: volumeName, Pool: pool, Seed: true})
	}

	detail, err := s.launchUnattendedVM(ctx, clusterName, mediaMember, pool, installerISO, seedISO, req)
	if err != nil {
		cleanup()
		return Detail{}, UnattendedLaunch{}, err
	}

	launch := UnattendedLaunch{
		Name:           req.Name,
		Media:          media,
		Profile:        profile,
		AnswerFileName: unattended.AnswerFileName(profile),
	}

	go s.watchUnattendedInstall(clusterName, req.Name, pool, media)

	return detail, launch, nil
}

// renderUnattendedAnswer produces the final answer file content: either the
// operator-provided override or the rendered built-in template.
func (s *Service) renderUnattendedAnswer(profile unattended.Profile, req UnattendedRequest) (string, error) {
	if strings.TrimSpace(req.AnswerFile) != "" {
		if strings.ContainsRune(req.AnswerFile, '\x00') {
			return "", fmt.Errorf("%w: answer file must be text", ErrInvalidRequest)
		}

		return req.AnswerFile, nil
	}

	content, err := unattended.RenderAnswerFile(profile, req.Spec)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	return content, nil
}

// remasterInstallerISO exports the source ISO, patches its boot
// configuration, adds the answer file, rebuilds the image and imports it as
// a new custom volume pinned to the source ISO's member. It returns the
// derived volume name.
func (s *Service) remasterInstallerISO(ctx context.Context, clusterName, member, pool, isoName string, profile unattended.Profile, answer, workDir string) (string, error) {
	if err := s.iso.Available(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnattended, err)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return "", err
	}

	volumeName := derivedISOName(isoName, profile)
	dir, err := os.MkdirTemp(workDir, "remaster-")
	if err != nil {
		return "", fmt.Errorf("%w: create work dir: %w", ErrUnattended, err)
	}
	defer os.RemoveAll(dir)

	srcPath := filepath.Join(dir, "src.iso")
	if err := s.exportISOVolume(ctx, client, pool, isoName, srcPath); err != nil {
		return "", err
	}

	patched := make(map[string]string, 2)
	for _, grubPath := range bootConfigPaths(profile) {
		localPath := filepath.Join(dir, fmt.Sprintf("grub-%d.cfg", len(patched)))
		content, err := s.iso.Extract(ctx, srcPath, grubPath, localPath)
		if err != nil {
			continue // optional location for this installer family
		}

		newContent, changed, err := unattended.PatchBootConfig(profile, content)
		if err != nil {
			return "", fmt.Errorf("%w: patch %s: %w", ErrUnattended, grubPath, err)
		}
		if changed == 0 {
			continue
		}

		// Write the patched content to a fresh file: xorriso extraction
		// preserves the read-only mode of files stored inside the ISO.
		patchedPath := filepath.Join(dir, fmt.Sprintf("patched-%d.cfg", len(patched)))
		if err := os.WriteFile(patchedPath, []byte(newContent), 0o600); err != nil {
			return "", fmt.Errorf("%w: write patched boot config: %w", ErrUnattended, err)
		}

		patched[grubPath] = patchedPath
	}
	if len(patched) == 0 {
		return "", fmt.Errorf("%w: no installer boot configuration (%s) found in %s",
			ErrUnattended, strings.Join(bootConfigPaths(profile), ", "), isoName)
	}

	answerName := unattended.AnswerFileName(profile)
	answerPath := filepath.Join(dir, answerName)
	if err := os.WriteFile(answerPath, []byte(answer), 0o600); err != nil {
		return "", fmt.Errorf("%w: write answer file: %w", ErrUnattended, err)
	}

	outPath := filepath.Join(dir, "out.iso")
	if err := s.iso.Rebuild(ctx, srcPath, outPath, patched, answerPath); err != nil {
		return "", err
	}

	// Integrity gate: the rebuilt image must carry the unattended
	// arguments before it becomes installation media.
	for grubPath := range patched {
		verifyPath := filepath.Join(dir, "verify.cfg")
		content, err := s.iso.Extract(ctx, outPath, grubPath, verifyPath)
		if err != nil {
			return "", fmt.Errorf("%w: verify %s: %w", ErrUnattended, grubPath, err)
		}
		if !strings.Contains(content, unattended.BootArgs(profile)) {
			return "", fmt.Errorf("%w: rebuilt %s does not carry unattended boot arguments", ErrUnattended, grubPath)
		}
	}

	outFile, err := os.Open(outPath) //nolint:gosec // path built inside the controlled work dir
	if err != nil {
		return "", fmt.Errorf("%w: open rebuilt iso: %w", ErrUnattended, err)
	}
	defer outFile.Close() //nolint:errcheck // read-only handle

	if _, err := s.importISOToMember(ctx, clusterName, member, pool, volumeName+".iso", outFile); err != nil {
		return "", fmt.Errorf("%w: import remastered iso: %w", ErrUnattended, err)
	}

	return volumeName, nil
}

// importISOToMember imports an ISO volume pinned to one cluster member so
// the media and the VM that boots it share a host.
func (s *Service) importISOToMember(ctx context.Context, clusterName, member, pool, filename string, reader io.Reader) (string, error) {
	if member == "" {
		return s.ImportISO(ctx, clusterName, pool, filename, reader)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return "", err
	}

	targeted, ok := any(client).(interface {
		UseTarget(name string) incus.InstanceServer
	})
	if !ok {
		return "", fmt.Errorf("%w: cluster client does not support targeting", ErrInvalidRequest)
	}

	name := strings.TrimSuffix(filename, ".iso")
	op, err := targeted.UseTarget(member).CreateStoragePoolVolumeFromISO(pool, incus.StorageVolumeBackupArgs{
		Name:       name,
		BackupFile: reader,
	})
	if err != nil {
		return "", fmt.Errorf("instances: import iso %s:%s: %w", clusterName, filename, err)
	}
	if err := waitOperation(ctx, op, "import iso "+name); err != nil {
		return "", err
	}

	return name, nil
}

// buildAndImportSeedISO renders the NoCloud seed ISO with the profile's seed
// files and imports it into the pool, pinned to the installer member. It
// returns the seed volume name.
func (s *Service) buildAndImportSeedISO(ctx context.Context, clusterName, member, pool, vmName string, files []unattended.SeedFile, workDir string) (string, error) {
	if err := s.iso.Available(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnattended, err)
	}

	dir, err := os.MkdirTemp(workDir, "seed-")
	if err != nil {
		return "", fmt.Errorf("%w: create seed work dir: %w", ErrUnattended, err)
	}
	defer os.RemoveAll(dir)

	seedPath, err := s.iso.BuildSeed(ctx, dir, files)
	if err != nil {
		return "", err
	}

	volumeName := "cidata-" + vmName
	file, err := os.Open(seedPath) //nolint:gosec // path returned by the iso tool inside the controlled work dir
	if err != nil {
		return "", fmt.Errorf("%w: open seed iso: %w", ErrUnattended, err)
	}
	defer file.Close() //nolint:errcheck // read-only handle

	if _, err := s.importISOToMember(ctx, clusterName, member, pool, volumeName+".iso", file); err != nil {
		return "", fmt.Errorf("%w: import seed iso: %w", ErrUnattended, err)
	}

	return volumeName, nil
}

// exportISOVolume writes the raw ISO bytes of one custom volume to destPath
// (volume backup export; for ISO volumes the backup is the ISO itself).
func (s *Service) exportISOVolume(ctx context.Context, client clusters.InstanceServer, pool, volume, destPath string) error {
	exportCtx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	backupName := fmt.Sprintf("export-%d", time.Now().UnixNano())
	op, err := client.CreateStorageVolumeBackup(pool, volume, api.StorageVolumeBackupsPost{
		Name:                 backupName,
		ExpiresAt:            time.Now().Add(24 * time.Hour),
		VolumeOnly:           true,
		CompressionAlgorithm: "none",
	})
	if err != nil {
		return fmt.Errorf("%w: create volume backup: %w", ErrUnattended, err)
	}
	if err := waitOperation(exportCtx, op, "backup volume "+volume); err != nil {
		return err
	}

	dest, err := os.Create(destPath) //nolint:gosec // path built inside the controlled work dir
	if err != nil {
		return fmt.Errorf("%w: create export file: %w", ErrUnattended, err)
	}

	_, err = client.GetStorageVolumeBackupFile(pool, volume, backupName, &incus.BackupFileRequest{
		BackupFile: dest,
	})
	closeErr := dest.Close()
	if err != nil {
		return fmt.Errorf("%w: download volume backup: %w", ErrUnattended, err)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: close export file: %w", ErrUnattended, closeErr)
	}

	delOp, delErr := client.DeleteStorageVolumeBackup(pool, volume, backupName)
	if delErr == nil && delOp != nil {
		_ = waitOperation(exportCtx, delOp, "delete temp backup")
	}

	return nil
}

// launchUnattendedVM creates the VM on the same member as its installation
// media. The installer ISO (remastered when the profile requires it) boots
// first; the NoCloud seed, when present, is attached as a second read-only
// media. -no-reboot makes the installer's final reboot power the VM off
// instead of looping back into the installer, which is the deterministic
// signal for the media watcher.
func (s *Service) launchUnattendedVM(ctx context.Context, clusterName, member, pool, installerISO, seedISO string, req UnattendedRequest) (Detail, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, err
	}

	var creator instanceCreator = client
	if member != "" {
		targeted, ok := any(client).(interface {
			UseTarget(name string) incus.InstanceServer
		})
		if !ok {
			return Detail{}, fmt.Errorf("%w: cluster client does not support targeting", ErrInvalidRequest)
		}
		creator = targeted.UseTarget(member)
	}

	root := map[string]string{"type": "disk", "path": "/", "pool": pool}
	if req.RootDisk != "" {
		root["size"] = req.RootDisk
	}
	devices := api.DevicesMap{
		"root": root,
		"iso0": {
			"type":          "disk",
			"pool":          pool,
			"source":        installerISO,
			"boot.priority": "1",
		},
	}
	if seedISO != "" {
		devices["iso1"] = map[string]string{
			"type":   "disk",
			"pool":   pool,
			"source": seedISO,
		}
	}

	if network := strings.TrimSpace(req.Network); network != "" {
		networks, err := client.GetNetworks()
		if err != nil {
			return Detail{}, fmt.Errorf("instances: list networks for %s: %w", clusterName, err)
		}

		var target *api.Network
		for i := range networks {
			if networks[i].Name == network {
				target = &networks[i]
				break
			}
		}
		if target == nil {
			return Detail{}, fmt.Errorf("%w: network %q does not exist on %s", ErrInvalidRequest, network, clusterName)
		}

		if target.Managed {
			devices["eth0"] = map[string]string{"type": "nic", "network": network}
		} else {
			devices["eth0"] = map[string]string{"type": "nic", "nictype": "macvlan", "parent": network}
		}
	}

	start := true
	if req.Start != nil {
		start = *req.Start
	}

	// Same console/secure-boot arming as LaunchFromISO, plus -no-reboot so
	// the finished install powers off instead of re-entering the installer.
	config := api.ConfigMap{}
	config["raw.qemu"] = fmt.Sprintf("%s unix=off,port=%d,disable-ticketing=on,addr=%s\n-no-reboot",
		spiceArgMarker, SPICEPortFor(req.Name), spiceListenAddr)
	config["security.secureboot"] = "false"

	post := api.InstancesPost{
		Name: req.Name,
		Type: api.InstanceTypeVM,
		InstancePut: api.InstancePut{
			Config:  config,
			Devices: devices,
		},
		Source: api.InstanceSource{Type: "none"},
		Start:  start,
	}

	createCtx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	op, err := creator.CreateInstance(post)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: create %s:%s: %w", clusterName, req.Name, err)
	}
	if err := waitOperation(createCtx, op, "create "+req.Name); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, req.Name)
}

// watchUnattendedInstall polls the instance until the installer finishes
// (the VM stops, thanks to -no-reboot) and then detaches the installation
// media and deletes the created volumes. If the instance is deleted first,
// the volumes are still cleaned up. The watch gives up after 6 hours and
// leaves the media for manual removal.
func (s *Service) watchUnattendedInstall(clusterName, instance, pool string, media []UnattendedMedia) {
	if len(media) == 0 {
		return
	}

	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}

	deadline := time.Now().Add(unattendedWatchTimeout)
	bootedAt := time.Time{}
	wasRunning := false

	for time.Now().Before(deadline) {
		time.Sleep(unattendedPollInterval)

		client, err := s.client(clusterName)
		if err != nil {
			logger.Warn("unattended watch: cluster client unavailable", "cluster", clusterName, "error", err)
			continue
		}

		state, _, err := client.GetInstanceState(instance)
		if err != nil {
			if isNotFound(err) {
				// Instance deleted mid-install: still remove the media
				// volumes so no answer-file copies linger.
				s.cleanupUnattendedMedia(client, pool, media)
				logger.Info("unattended watch: instance deleted, media volumes cleaned", "cluster", clusterName, "instance", instance)
				return
			}
			logger.Warn("unattended watch: state poll failed", "cluster", clusterName, "instance", instance, "error", err)
			continue
		}

		switch state.StatusCode {
		case api.Running:
			if !wasRunning {
				bootedAt = time.Now()
			}
			wasRunning = true
		case api.Stopped:
			if !wasRunning || time.Since(bootedAt) < minBootAge {
				// Never really ran (or stopped immediately): leave the
				// media so the launch can simply be started again.
				continue
			}
			s.detachUnattendedMedia(clusterName, instance, media)
			s.cleanupUnattendedMedia(client, pool, media)
			logger.Info("unattended watch: install finished, media detached", "cluster", clusterName, "instance", instance, "media", len(media))
			return
		}
	}

	logger.Warn("unattended watch: timeout, installation media left attached", "cluster", clusterName, "instance", instance, "media", len(media))
}

// detachUnattendedMedia removes the media devices from the instance.
func (s *Service) detachUnattendedMedia(clusterName, instance string, media []UnattendedMedia) {
	for _, item := range media {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if _, err := s.DetachDevice(ctx, clusterName, instance, item.Device); err != nil && s.logger != nil {
			s.logger.Warn("unattended watch: media detach failed", "cluster", clusterName, "instance", instance, "device", item.Device, "error", err)
		}
		cancel()
	}
}

// cleanupUnattendedMedia deletes the volumes created for the install.
func (s *Service) cleanupUnattendedMedia(client clusters.InstanceServer, pool string, media []UnattendedMedia) {
	for _, item := range media {
		if err := client.DeleteStoragePoolVolume(pool, isoVolumeType, item.Volume); err != nil && s.logger != nil {
			s.logger.Warn("unattended media volume cleanup failed", "pool", pool, "volume", item.Volume, "error", err)
		}
	}
}

// unattendedWorkDir resolves and creates the scratch directory for ISO
// remastering. It must live on a writable path of the fleet server.
func (s *Service) unattendedWorkDir() (string, error) {
	dir := s.workDir
	if dir == "" {
		dir = filepath.Join("data", "unattended")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("%w: work dir %s: %w", ErrUnattended, dir, err)
	}

	return dir, nil
}
