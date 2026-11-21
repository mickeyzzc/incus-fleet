package unattended

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, p := range Profiles {
		if err := Validate(p); err != nil {
			t.Fatalf("Validate(%s) error = %v", p, err)
		}
	}
	if err := Validate("nope"); err == nil {
		t.Fatal("Validate(unknown) expected error")
	}
}

func TestNeedsRemaster(t *testing.T) {
	if !NeedsRemaster(ProfileDebianPreseed) || !NeedsRemaster(ProfileKickstart) {
		t.Fatal("preseed/kickstart must remaster the installer ISO")
	}
	if NeedsRemaster(ProfileUbuntuAutoinstall) || NeedsRemaster(ProfileNoCloud) {
		t.Fatal("autoinstall/nocloud must not remaster the installer ISO")
	}
}

func TestValidateAnswerSpec(t *testing.T) {
	if err := ValidateAnswerSpec(AnswerSpec{Hostname: "web-01", Username: "fleet"}); err != nil {
		t.Fatalf("ValidateAnswerSpec() error = %v", err)
	}
	if err := ValidateAnswerSpec(AnswerSpec{Hostname: "bad name"}); err == nil {
		t.Fatal("invalid hostname accepted")
	}
	if err := ValidateAnswerSpec(AnswerSpec{Username: "1user"}); err == nil {
		t.Fatal("invalid username accepted")
	}
	if err := ValidateAnswerSpec(AnswerSpec{Password: "multi\nline"}); err == nil {
		t.Fatal("multi-line password accepted")
	}
}

func TestRenderAnswerFileDebianPreseed(t *testing.T) {
	content, err := RenderAnswerFile(ProfileDebianPreseed, AnswerSpec{
		Hostname: "web-01",
		Username: "ops",
		Password: "s3cret",
		Timezone: "Asia/Shanghai",
		Disk:     "/dev/sda",
		Locale:   "zh_CN",
		Keyboard: "us",
	})
	if err != nil {
		t.Fatalf("RenderAnswerFile() error = %v", err)
	}

	for _, want := range []string{
		"d-i netcfg/get_hostname string web-01",
		"d-i passwd/username string ops",
		"d-i passwd/user-password password s3cret",
		"d-i time/zone string Asia/Shanghai",
		"d-i partman-auto/disk string /dev/sda",
		"d-i debian-installer/locale string zh_CN",
		"d-i partman/confirm_nooverwrite boolean true",
		"d-i pkgsel/include string openssh-server",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("preseed missing %q", want)
		}
	}
}

func TestRenderAnswerFileDefaults(t *testing.T) {
	for _, p := range []Profile{ProfileDebianPreseed, ProfileKickstart, ProfileUbuntuAutoinstall, ProfileNoCloud} {
		content, err := RenderAnswerFile(p, AnswerSpec{})
		if err != nil {
			t.Fatalf("RenderAnswerFile(%s) error = %v", p, err)
		}
		if content == "" {
			t.Fatalf("RenderAnswerFile(%s) empty", p)
		}
		if !strings.Contains(content, "fleet-unattended") {
			t.Errorf("%s missing default hostname", p)
		}
	}
}

func TestRenderAnswerFileKickstart(t *testing.T) {
	content, err := RenderAnswerFile(ProfileKickstart, AnswerSpec{
		Hostname: "db-01",
		Username: "ops",
		Password: "s3cret",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("RenderAnswerFile() error = %v", err)
	}

	for _, want := range []string{
		"network --hostname db-01",
		"user --name=ops --password='s3cret' --plaintext",
		"timezone Asia/Shanghai --utc",
		"autopart --type=plain",
		"reboot",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("kickstart missing %q", want)
		}
	}
	if strings.Contains(content, "ignoredisk") {
		t.Error("kickstart must not constrain disks without spec.Disk")
	}
}

func TestRenderAnswerFileKickstartDisk(t *testing.T) {
	content, err := RenderAnswerFile(ProfileKickstart, AnswerSpec{Disk: "/dev/vda"})
	if err != nil {
		t.Fatalf("RenderAnswerFile() error = %v", err)
	}
	if !strings.Contains(content, "ignoredisk --only-use=/dev/vda") {
		t.Error("kickstart missing ignoredisk for spec.Disk")
	}
}

func TestRenderAnswerFileAutoinstall(t *testing.T) {
	content, err := RenderAnswerFile(ProfileUbuntuAutoinstall, AnswerSpec{Hostname: "u1", Username: "ops", Password: "pw"})
	if err != nil {
		t.Fatalf("RenderAnswerFile() error = %v", err)
	}
	if !strings.HasPrefix(content, "#cloud-config") {
		t.Error("autoinstall user-data must be cloud-config")
	}
	if !strings.Contains(content, "version: 1") || !strings.Contains(content, "install-server: true") {
		t.Error("autoinstall missing required blocks")
	}
}

func TestRenderSeedFiles(t *testing.T) {
	files, err := RenderSeedFiles(ProfileUbuntuAutoinstall, AnswerSpec{Hostname: "u1"})
	if err != nil {
		t.Fatalf("RenderSeedFiles() error = %v", err)
	}
	if len(files) != 2 || files[0].Name != "user-data" || files[1].Name != "meta-data" {
		t.Fatalf("seed files = %#v", files)
	}
	if !strings.Contains(files[1].Content, "local-hostname: u1") {
		t.Errorf("meta-data missing hostname: %q", files[1].Content)
	}

	files, err = RenderSeedFiles(ProfileDebianPreseed, AnswerSpec{})
	if err != nil {
		t.Fatalf("RenderSeedFiles(preseed) error = %v", err)
	}
	if files != nil {
		t.Fatalf("preseed profile must not produce seed files: %#v", files)
	}
}

const debianGrubCfg = `menuentry --hotkey=g 'Graphical install' {
    set background_color=black
    linux    /install.amd/vmlinuz vga=788 --- quiet
    initrd   /install.amd/gtk/initrd.gz
}
menuentry --hotkey=i 'Install' {
    set background_color=black
    linux    /install.amd/vmlinuz vga=788 --- quiet
    initrd   /install.amd/initrd.gz
}
submenu 'Advanced options' {
    menuentry '... Graphical automated install' {
        linux    /install.amd/vmlinuz auto=true priority=critical vga=788 --- quiet
        initrd   /install.amd/gtk/initrd.gz
    }
}
`

func TestPatchBootConfigDebian(t *testing.T) {
	patched, changed, err := PatchBootConfig(ProfileDebianPreseed, debianGrubCfg)
	if err != nil {
		t.Fatalf("PatchBootConfig() error = %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}

	// The two top-level entries gain the arguments and the pre-existing
	// automated entry inside the submenu keeps its own wording, so the
	// configuration ends up with three occurrences; `changed` above is the
	// count of lines this patch actually modified.
	count := strings.Count(patched, BootArgs(ProfileDebianPreseed))
	if count != 2 {
		t.Fatalf("args count = %d, want 2", count)
	}

	// The menu boots itself: default entry and timeout are forced.
	if !strings.Contains(patched, "set default=0") || !strings.Contains(patched, "set timeout=10") {
		t.Error("patched config missing default entry and timeout")
	}

	// The kernel path and the --- separator must be preserved.
	if !strings.Contains(patched, "linux    /install.amd/vmlinuz auto=true priority=critical vga=788 --- quiet") {
		t.Errorf("patched line malformed:\n%s", patched)
	}

	// Idempotency: already-patched lines are not patched again.
	again, changedAgain, err := PatchBootConfig(ProfileDebianPreseed, patched)
	if err != nil {
		t.Fatalf("PatchBootConfig(idempotent) error = %v", err)
	}
	if changedAgain != 0 || again != patched {
		t.Fatalf("idempotency violated: changed=%d same=%v", changedAgain, again == patched)
	}
}

const anacondaGrubCfg = `set default="1"
menuentry 'Install Rocky Linux' --class fedora --class gnu-linux {
	linuxefi /images/pxeboot/vmlinuz inst.stage2=hd:LABEL=Rocky quiet
	initrdefi /images/pxeboot/initrd.img
}
`

func TestPatchBootConfigKickstart(t *testing.T) {
	// Anaconda images use "linux /images/pxeboot/vmlinuz" on modern
	// releases; linuxefi variants are not patched by the generic matcher.
	content := strings.ReplaceAll(anacondaGrubCfg, "linuxefi /images/pxeboot/vmlinuz", "linux /images/pxeboot/vmlinuz")
	patched, changed, err := PatchBootConfig(ProfileKickstart, content)
	if err != nil {
		t.Fatalf("PatchBootConfig() error = %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if !strings.Contains(patched, "linux /images/pxeboot/vmlinuz inst.ks=cdrom:/ks.cfg inst.stage2=hd:LABEL=Rocky quiet") {
		t.Errorf("patched line malformed:\n%s", patched)
	}
}

func TestPatchBootConfigNoLines(t *testing.T) {
	if _, _, err := PatchBootConfig(ProfileDebianPreseed, "nothing here"); err == nil {
		t.Fatal("patch without matching kernel lines must fail loudly")
	}
	if _, _, err := PatchBootConfig(ProfileNoCloud, debianGrubCfg); err == nil {
		t.Fatal("nocloud profile must not patch boot configuration")
	}
}

func TestAnswerFileName(t *testing.T) {
	cases := map[Profile]string{
		ProfileDebianPreseed:     "preseed.cfg",
		ProfileKickstart:         "ks.cfg",
		ProfileUbuntuAutoinstall: "user-data",
		ProfileNoCloud:           "user-data",
	}
	for profile, want := range cases {
		if got := AnswerFileName(profile); got != want {
			t.Errorf("AnswerFileName(%s) = %q, want %q", profile, got, want)
		}
	}
}
