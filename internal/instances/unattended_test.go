package instances

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
	"github.com/mickeyzzc/incus-fleet/internal/unattended"
)

// stubISOTool simulates the on-disk ISO surgery without xorriso. The "ISO"
// is a text file: extraction reads patched markers, rebuild appends them.
type stubISOTool struct {
	availableErr error
	extractErr   error
	rebuildErr   error
	seedErr      error
	rebuildCalls int
	seedCalls    int
}

func (t *stubISOTool) Available() error { return t.availableErr }

func (t *stubISOTool) Extract(_ context.Context, isoPath, isoFilePath, destPath string) (string, error) {
	if t.extractErr != nil {
		return "", t.extractErr
	}

	data, err := os.ReadFile(isoPath)
	if err != nil {
		return "", err
	}

	// Simulate the round trip: the rebuilt image contains the patched boot
	// configuration, the original does not.
	content := string(data)
	if strings.Contains(content, "[rebuilt]") {
		return content, nil
	}

	switch isoFilePath {
	case "/boot/grub/grub.cfg":
		return debianGrubFixture, nil
	case "/EFI/BOOT/grub.cfg":
		return anacondaGrubFixture, nil
	default:
		return "", fmt.Errorf("no %s in image", isoFilePath)
	}
}

func (t *stubISOTool) Rebuild(_ context.Context, srcPath, dstPath string, patched map[string]string, extraPath string) error {
	if t.rebuildErr != nil {
		return t.rebuildErr
	}
	t.rebuildCalls++

	if _, err := os.Stat(srcPath); err != nil {
		return err
	}

	var out strings.Builder
	out.WriteString("[rebuilt]\n")
	for isoPath, localPath := range patched {
		content, err := os.ReadFile(localPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "[%s]\n%s", isoPath, content)
	}
	answer, err := os.ReadFile(extraPath)
	if err != nil {
		return err
	}
	out.WriteString("[answer]\n")
	out.Write(answer)

	return os.WriteFile(dstPath, []byte(out.String()), 0o600)
}

func (t *stubISOTool) BuildSeed(_ context.Context, dir string, files []unattended.SeedFile) (string, error) {
	if t.seedErr != nil {
		return "", t.seedErr
	}
	t.seedCalls++

	var out strings.Builder
	for _, file := range files {
		out.WriteString("[" + file.Name + "]\n")
		out.WriteString(file.Content)
	}

	path := filepath.Join(dir, "cidata.iso")
	if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
		return "", err
	}

	return path, nil
}

const debianGrubFixture = `menuentry --hotkey=g 'Graphical install' {
    linux    /install.amd/vmlinuz vga=788 --- quiet
    initrd   /install.amd/gtk/initrd.gz
}
menuentry --hotkey=i 'Install' {
    linux    /install.amd/vmlinuz vga=788 --- quiet
    initrd   /install.amd/initrd.gz
}
`

const anacondaGrubFixture = `menuentry 'Install' {
	linux /images/pxeboot/vmlinuz inst.stage2=hd:LABEL=Rocky quiet
}
`

func TestLaunchUnattendedPreseedRemaster(t *testing.T) {
	source := &incusfake.Server{
		PoolNames:      []string{"default"},
		ISOExportBytes: []byte("original-iso-bytes"),
		Instance:       &api.Instance{Name: "web-01"},
		Volumes:        []api.StorageVolume{{Name: "debian-13.7.0-amd64-DVD-1", Type: "custom", ContentType: "iso", Location: "n305-server"}},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source},
		withIsoTool(&stubISOTool{}),
		withUnattendedWorkDirForTest(t),
	)

	_, launch, err := svc.LaunchUnattended(context.Background(), "c1", "default", "debian-13.7.0-amd64-DVD-1", UnattendedRequest{
		Name:    "web-01",
		Profile: string(unattended.ProfileDebianPreseed),
		Spec: unattended.AnswerSpec{
			Hostname: "web-01",
			Username: "ops",
			Password: "s3cret",
		},
	})
	if err != nil {
		t.Fatalf("LaunchUnattended() error = %v", err)
	}

	// The derived installer volume is imported and attached as iso0.
	imports := source.CreateISOArgs()
	if len(imports) != 1 || imports[0].Name != "debian-13.7.0-amd64-DVD-1-debian-preseed-auto" {
		t.Fatalf("iso imports = %#v", imports)
	}

	// The VM carries the derived installer, -no-reboot and secureboot off.
	creates := source.CreateArgs()
	if len(creates) != 1 {
		t.Fatalf("create calls = %d", len(creates))
	}
	if creates[0].Name != "web-01" || creates[0].Type != api.InstanceTypeVM {
		t.Fatalf("created = %#v", creates[0])
	}
	if strings.Contains(creates[0].Config["raw.qemu"], "-no-reboot") {
		// Incus resets the VM at start and owns the QMP actions; the flag
		// breaks VM startup ("Failed setting reboot action").
		t.Errorf("raw.qemu must not carry -no-reboot: %q", creates[0].Config["raw.qemu"])
	}
	if creates[0].Config["security.secureboot"] != "false" {
		t.Errorf("secureboot = %q", creates[0].Config["security.secureboot"])
	}
	iso0 := creates[0].Devices["iso0"]
	if iso0["source"] != "debian-13.7.0-amd64-DVD-1-debian-preseed-auto" || iso0["boot.priority"] != "1" {
		t.Fatalf("iso0 device = %#v", iso0)
	}
	if _, hasSeed := creates[0].Devices["iso1"]; hasSeed {
		t.Error("preseed profile must not attach a seed ISO")
	}

	if launch.Profile != unattended.ProfileDebianPreseed || launch.AnswerFileName != "preseed.cfg" {
		t.Fatalf("launch = %#v", launch)
	}
	if len(launch.Media) != 1 || launch.Media[0].Device != "iso0" {
		t.Fatalf("media = %#v", launch.Media)
	}

	// The export pipeline ran exactly once against the source volume.
	exports := source.ExportBackupArgs()
	if len(exports) != 1 || exports[0].Volume != "debian-13.7.0-amd64-DVD-1" {
		t.Fatalf("exports = %#v", exports)
	}
}

func TestLaunchUnattendedNoCloudSeed(t *testing.T) {
	source := &incusfake.Server{
		PoolNames: []string{"default"},
		Instance:  &api.Instance{Name: "u-01"},
		Volumes:   []api.StorageVolume{{Name: "ubuntu-24.04-live-server", Type: "custom", ContentType: "iso", Location: "n305-server"}},
	}
	tool := &stubISOTool{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source},
		withIsoTool(tool),
		withUnattendedWorkDirForTest(t),
	)

	_, launch, err := svc.LaunchUnattended(context.Background(), "c1", "default", "ubuntu-24.04-live-server", UnattendedRequest{
		Name:    "u-01",
		Profile: string(unattended.ProfileNoCloud),
		Spec:    unattended.AnswerSpec{Hostname: "u-01"},
	})
	if err != nil {
		t.Fatalf("LaunchUnattended() error = %v", err)
	}

	if tool.seedCalls != 1 || tool.rebuildCalls != 0 {
		t.Fatalf("tool calls: rebuild=%d seed=%d", tool.rebuildCalls, tool.seedCalls)
	}

	// Two imports: untouched installer stays, only the seed is new.
	imports := source.CreateISOArgs()
	if len(imports) != 1 || imports[0].Name != "cidata-u-01" {
		t.Fatalf("iso imports = %#v", imports)
	}

	creates := source.CreateArgs()
	if len(creates) != 1 {
		t.Fatalf("create calls = %d", len(creates))
	}
	if creates[0].Devices["iso0"]["source"] != "ubuntu-24.04-live-server" {
		t.Fatalf("iso0 = %#v", creates[0].Devices["iso0"])
	}
	if creates[0].Devices["iso1"]["source"] != "cidata-u-01" {
		t.Fatalf("iso1 = %#v", creates[0].Devices["iso1"])
	}
	if len(launch.Media) != 1 || !launch.Media[0].Seed {
		t.Fatalf("media = %#v", launch.Media)
	}
}

func TestLaunchUnattendedValidation(t *testing.T) {
	source := &incusfake.Server{
		PoolNames: []string{"default"},
		Instance:  &api.Instance{Name: "u-01"},
	}
	tool := &stubISOTool{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source},
		withIsoTool(tool),
		withUnattendedWorkDirForTest(t),
	)
	ctx := context.Background()

	cases := []struct {
		name string
		req  UnattendedRequest
	}{
		{"unknown profile", UnattendedRequest{Name: "a", Profile: "magic"}},
		{"missing name", UnattendedRequest{Profile: "nocloud"}},
		{"bad instance name", UnattendedRequest{Name: "bad name", Profile: "nocloud"}},
		{"bad hostname", UnattendedRequest{Name: "a", Profile: "nocloud", Spec: unattended.AnswerSpec{Hostname: "no no"}}},
		{"bad username", UnattendedRequest{Name: "a", Profile: "nocloud", Spec: unattended.AnswerSpec{Username: "9bad"}}},
	}
	for _, tc := range cases {
		if _, _, err := svc.LaunchUnattended(ctx, "c1", "default", "iso", tc.req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: error = %v, want ErrInvalidRequest", tc.name, err)
		}
	}

	// Unknown pool.
	if _, _, err := svc.LaunchUnattended(ctx, "c1", "missing", "iso", UnattendedRequest{Name: "a", Profile: "nocloud"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown pool: error = %v", err)
	}

	// Missing xorriso-equivalent tool.
	toolSource := &incusfake.Server{
		PoolNames: []string{"default"},
		Volumes:   []api.StorageVolume{{Name: "iso", Type: "custom", ContentType: "iso", Location: "n305-server"}},
	}
	failing := &stubISOTool{availableErr: errors.New("no xorriso")}
	svc2 := newTestService(t, map[string]*incusfake.Server{"c1": toolSource},
		withIsoTool(failing),
		withUnattendedWorkDirForTest(t),
	)
	if _, _, err := svc2.LaunchUnattended(ctx, "c1", "default", "iso", UnattendedRequest{Name: "a", Profile: "debian-preseed"}); !errors.Is(err, ErrUnattended) {
		t.Errorf("missing tool: error = %v", err)
	}
}

func TestLaunchUnattendedCustomAnswerFile(t *testing.T) {
	source := &incusfake.Server{
		PoolNames:      []string{"default"},
		ISOExportBytes: []byte("iso"),
		Instance:       &api.Instance{Name: "cust-01"},
		Volumes:        []api.StorageVolume{{Name: "debian", Type: "custom", ContentType: "iso", Location: "n305-server"}},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source},
		withIsoTool(&stubISOTool{}),
		withUnattendedWorkDirForTest(t),
	)

	answer := "# my preseed\nd-i debian-installer/locale string en_US\n"
	_, launch, err := svc.LaunchUnattended(context.Background(), "c1", "default", "debian", UnattendedRequest{
		Name:       "cust-01",
		Profile:    string(unattended.ProfileDebianPreseed),
		AnswerFile: answer,
	})
	if err != nil {
		t.Fatalf("LaunchUnattended() error = %v", err)
	}
	if launch.AnswerFileName != "preseed.cfg" {
		t.Fatalf("answer file name = %q", launch.AnswerFileName)
	}
}

func TestWatchUnattendedInstallDetachesMedia(t *testing.T) {
	source := &incusfake.Server{
		PoolNames: []string{"default"},
		Instance: &api.Instance{
			Name: "vm-1",
			InstancePut: api.InstancePut{
				Devices: api.DevicesMap{
					"iso0": map[string]string{"type": "disk", "source": "derived-auto"},
				},
			},
		},
	}

	// Installer ran and the VM is now stopped (post-install poweroff).
	running := &api.InstanceState{StatusCode: api.Running}
	stopped := &api.InstanceState{StatusCode: api.Stopped}
	states := []*api.InstanceState{running, stopped}
	source.StateFunc = func(string) (*api.InstanceState, error) {
		if len(states) == 0 {
			return stopped, nil
		}
		next := states[0]
		states = states[1:]
		return next, nil
	}

	svc := newTestService(t, map[string]*incusfake.Server{"c1": source}, withUnattendedWorkDirForTest(t))
	defer fastUnattendedWatch(t)()

	media := []UnattendedMedia{{Device: "iso0", Volume: "derived-auto", Pool: "default"}}
	svc.watchUnattendedInstall("c1", "vm-1", "default", media)

	deletes := source.DeleteVolumeArgs()
	if len(deletes) != 1 || deletes[0].Name != "derived-auto" {
		t.Fatalf("volume deletes = %#v", deletes)
	}
	detaches := source.UpdateArgs()
	if len(detaches) == 0 {
		t.Fatal("expected a device update (detach)")
	}
}

func TestWatchUnattendedInstallInstanceDeleted(t *testing.T) {
	source := &incusfake.Server{
		PoolNames: []string{"default"},
		StateErr:  api.StatusErrorf(http.StatusNotFound, "missing"),
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": source}, withUnattendedWorkDirForTest(t))
	restore := fastUnattendedWatch(t)
	t.Cleanup(restore)
	unattendedWatchTimeout = 3 * unattendedPollInterval

	media := []UnattendedMedia{{Device: "iso1", Volume: "cidata-vm", Pool: "default"}}
	svc.watchUnattendedInstall("c1", "gone", "default", media)

	deletes := source.DeleteVolumeArgs()
	if len(deletes) != 1 || deletes[0].Name != "cidata-vm" {
		t.Fatalf("volume deletes = %#v", deletes)
	}
}

func TestWatchUnattendedInstallNeverBooted(t *testing.T) {
	source := &incusfake.Server{PoolNames: []string{"default"}}
	// Always stopped, never ran: media must survive.
	source.StateFunc = func(string) (*api.InstanceState, error) {
		return &api.InstanceState{StatusCode: api.Stopped}, nil
	}

	svc := newTestService(t, map[string]*incusfake.Server{"c1": source}, withUnattendedWorkDirForTest(t))
	restore := fastUnattendedWatch(t)
	t.Cleanup(restore)
	unattendedWatchTimeout = 3 * unattendedPollInterval

	svc.watchUnattendedInstall("c1", "vm-x", "default", []UnattendedMedia{{Device: "iso0", Volume: "keep-auto", Pool: "default"}})

	if deletes := source.DeleteVolumeArgs(); len(deletes) != 0 {
		t.Fatalf("media deleted for never-booted install: %#v", deletes)
	}
}

// fastUnattendedWatch shortens the watch cycle for tests and restores the
// production values when the test finishes.
func fastUnattendedWatch(t *testing.T) func() {
	t.Helper()

	originalTimeout := unattendedWatchTimeout
	originalPoll := unattendedPollInterval
	originalBoot := minBootAge

	unattendedPollInterval = 5 * time.Millisecond
	minBootAge = 0

	return func() {
		unattendedWatchTimeout = originalTimeout
		unattendedPollInterval = originalPoll
		minBootAge = originalBoot
	}
}

// withUnattendedWorkDirForTest points the service scratch dir at t.TempDir().
func withUnattendedWorkDirForTest(t *testing.T) Option {
	t.Helper()

	return WithUnattendedWorkDir(t.TempDir())
}
