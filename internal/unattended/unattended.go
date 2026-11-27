// Package unattended renders answer files and boot configuration for
// unattended (kickstart/preseed/autoinstall) OS installations from ISO.
//
// The pure logic in this package has no I/O: the instances service feeds it
// installer boot configuration and receives the artifacts it must place on
// the installation media.
package unattended

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Profile is a family of unattended installers supported by LaunchUnattended.
type Profile string

const (
	// ProfileDebianPreseed covers Debian and Debian-family installers that
	// read a d-i preseed file (Debian, and derivatives shipping d-i).
	ProfileDebianPreseed Profile = "debian-preseed"
	// ProfileUbuntuAutoinstall covers Subiquity-based Ubuntu server
	// installers reading user-data/meta-data from a NoCloud seed.
	ProfileUbuntuAutoinstall Profile = "ubuntu-autoinstall"
	// ProfileKickstart covers Anaconda-based installers (RHEL, Fedora,
	// CentOS, Rocky, Alma) reading a kickstart file.
	ProfileKickstart Profile = "kickstart"
	// ProfileNoCloud is the generic cloud-init NoCloud seed for custom
	// ISOs that honor cloud-init at install or first-boot time.
	ProfileNoCloud Profile = "nocloud"
)

// Profiles lists the selectable profiles in UI order.
var Profiles = []Profile{ProfileDebianPreseed, ProfileUbuntuAutoinstall, ProfileKickstart, ProfileNoCloud}

// Validate reports whether the profile is known.
func Validate(p Profile) error {
	switch p {
	case ProfileDebianPreseed, ProfileUbuntuAutoinstall, ProfileKickstart, ProfileNoCloud:
		return nil
	default:
		return fmt.Errorf("unattended: unknown profile %q", string(p))
	}
}

// NeedsRemaster reports whether the profile injects installer arguments via
// the ISO boot configuration (true) or only needs a NoCloud seed media
// attached next to the untouched installer ISO (false).
func NeedsRemaster(p Profile) bool {
	switch p {
	case ProfileDebianPreseed, ProfileKickstart:
		return true
	default:
		return false
	}
}

// NeedsBootPatch reports whether the ISO boot configuration must be patched.
// Ubuntu keeps the attached NoCloud seed but still needs the "autoinstall"
// kernel argument, otherwise Subiquity asks for confirmation.
func NeedsBootPatch(p Profile) bool {
	return NeedsRemaster(p) || p == ProfileUbuntuAutoinstall
}

// AnswerSpec are the template fields for built-in answer files.
type AnswerSpec struct {
	Hostname string
	Username string
	Password string
	Timezone string
	// Disk is the target disk device for partitioning (empty picks the
	// installer's default first disk).
	Disk string
	// Locale in installer locale form (for example en_US or zh_CN).
	Locale string
	// Keyboard layout (for example us).
	Keyboard string
	// Packages is an optional space-separated list of extra packages the
	// installer should add to the base system.
	Packages string
}

var (
	hostnameRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	usernameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}[$]?$`)
)

// ValidateAnswerSpec checks the operator-provided fields before they are
// embedded into installer artifacts.
func ValidateAnswerSpec(spec AnswerSpec) error {
	if spec.Hostname != "" && !hostnameRE.MatchString(spec.Hostname) {
		return fmt.Errorf("unattended: invalid hostname %q", spec.Hostname)
	}
	if spec.Username != "" && !usernameRE.MatchString(spec.Username) {
		return fmt.Errorf("unattended: invalid username %q", spec.Username)
	}
	if strings.ContainsAny(spec.Password, "\n\r") {
		return errors.New("unattended: password must not contain line breaks")
	}
	return nil
}

// AnswerFileName is the on-media answer file name per profile.
func AnswerFileName(p Profile) string {
	switch p {
	case ProfileDebianPreseed:
		return "preseed.cfg"
	case ProfileKickstart:
		return "ks.cfg"
	case ProfileUbuntuAutoinstall, ProfileNoCloud:
		return "user-data"
	default:
		return "preseed.cfg"
	}
}

// RenderAnswerFile produces the answer file content for the profile from the
// built-in template. The rendered file is placed on the installation media.
func RenderAnswerFile(p Profile, spec AnswerSpec) (string, error) {
	if err := ValidateAnswerSpec(spec); err != nil {
		return "", err
	}
	switch p {
	case ProfileDebianPreseed:
		return renderPreseed(spec), nil
	case ProfileKickstart:
		return renderKickstart(spec), nil
	case ProfileUbuntuAutoinstall:
		return renderAutoinstall(spec)
	case ProfileNoCloud:
		return renderCloudConfig(spec), nil
	default:
		return "", fmt.Errorf("unattended: unknown profile %q", string(p))
	}
}

// SeedFile is one file inside the NoCloud seed ISO.
type SeedFile struct {
	Name    string
	Content string
}

// BootArgs renders the kernel command line fragment injected into the
// installer's boot configuration. Debian preseeds ride inside the initrd
// (d-i's S30initrd-preseed loads /preseed.cfg from the initramfs before the
// installer even starts): a file=/cdrom/... argument cannot work there — the
// preseed fetcher runs before the medium is mounted — so only the auto
// flags live on the command line.
func BootArgs(p Profile) string {
	switch p {
	case ProfileDebianPreseed:
		return "auto=true priority=critical file=/preseed.cfg"
	case ProfileKickstart:
		return "inst.ks=cdrom:/ks.cfg"
	case ProfileUbuntuAutoinstall:
		return "autoinstall"
	default:
		return ""
	}
}

// InitrdPaths are the installer initrds to patch for profiles that carry
// their answer file inside the initramfs. Debian ships two: the text-mode
// installer and the GTK one used by the default (Graphical install) boot
// entry - both must carry the seed.
func InitrdPaths(p Profile) []string {
	switch p {
	case ProfileDebianPreseed:
		return []string{"/install.amd/gtk/initrd.gz", "/install.amd/initrd.gz"}
	default:
		return nil
	}
}

// InitrdSeedName is the answer file name inside the initrd segment.
func InitrdSeedName(p Profile) string {
	switch p {
	case ProfileDebianPreseed:
		return "preseed.cfg"
	default:
		return ""
	}
}

// PatchBootConfig injects the profile's unattended arguments into a GRUB
// boot configuration. Kernel lines matching the profile's loader path gain
// the unattended arguments, and a default entry with a short timeout is
// forced at the top so the menu boots without keyboard input (installer
// ISOs ship GRUB menus that wait for Enter). Lines already carrying
// unattended arguments are left untouched so the function is idempotent.
// It returns the patched configuration and the number of kernel lines
// changed.
func PatchBootConfig(p Profile, content string) (string, int, error) {
	args := BootArgs(p)
	if args == "" {
		return content, 0, fmt.Errorf("unattended: profile %q does not patch boot configuration", string(p))
	}

	var kernelRE *regexp.Regexp
	switch p {
	case ProfileDebianPreseed:
		// linux    /install.amd/vmlinuz vga=788 --- quiet
		kernelRE = regexp.MustCompile(`^(\s*linux\s+/install\.[a-z0-9]+/vmlinuz\s+)`)
	case ProfileKickstart:
		// linux /images/pxeboot/vmlinuz inst.stage2=... quiet
		kernelRE = regexp.MustCompile(`^(\s*linux\s+/images/pxeboot/vmlinuz\s+)`)
	case ProfileUbuntuAutoinstall:
		// linux /casper/vmlinuz --- quiet splash
		kernelRE = regexp.MustCompile(`^(\s*linux\s+/casper/vmlinuz\s+)`)
	default:
		return content, 0, fmt.Errorf("unattended: profile %q does not patch boot configuration", string(p))
	}

	changed := 0
	alreadyPatched := 0
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, "preseed") || strings.Contains(line, "inst.ks") ||
			strings.Contains(line, "auto=true") || strings.Contains(line, "autoinstall") {
			if kernelRE.MatchString(line) {
				alreadyPatched++
			}
			continue
		}
		match := kernelRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		// Insert after the kernel path, preserving the rest of the line
		// (--- separator and trailing options).
		rest := strings.TrimRight(line[len(match[1]):], " ")
		lines[i] = strings.TrimRight(match[1]+args+" "+rest, " ")
		changed++
	}

	if changed == 0 {
		if alreadyPatched > 0 {
			// Idempotent re-entry: the configuration already carries the
			// unattended arguments.
			return content, 0, nil
		}
		return content, 0, fmt.Errorf("unattended: no %s kernel lines found to patch", string(p))
	}

	// Boot the default entry automatically: installer menus otherwise wait
	// for keyboard input forever.
	header := []string{"set default=0", "set timeout=10", ""}
	out := make([]string, 0, len(header)+len(lines))
	out = append(out, header...)
	out = append(out, lines...)

	return strings.Join(out, "\n"), changed, nil
}

// RenderSeedFiles produces the NoCloud seed media files for profiles that
// consume a cidata media (autoinstall needs meta-data next to user-data).
// Remaster profiles (preseed/kickstart) carry the answer file inside the
// patched installer ISO and produce no seed files.
//
// answerOverride, when non-empty, replaces the rendered user-data content;
// the operator's custom answer file must reach the seed media exactly as the
// preview endpoint showed it. meta-data always comes from spec (the seed's
// identity is independent of the answer file).
func RenderSeedFiles(p Profile, spec AnswerSpec, answerOverride string) ([]SeedFile, error) {
	if NeedsRemaster(p) {
		return nil, nil
	}

	content := strings.TrimSpace(answerOverride)
	if content == "" {
		rendered, err := RenderAnswerFile(p, spec)
		if err != nil {
			return nil, err
		}
		content = rendered
	}
	return []SeedFile{
		{Name: "user-data", Content: content},
		{Name: "meta-data", Content: renderMetaData(spec)},
	}, nil
}

func renderMetaData(spec AnswerSpec) string {
	host := spec.Hostname
	if host == "" {
		host = "fleet-unattended"
	}
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", host, host)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func renderPreseed(spec AnswerSpec) string {
	locale := firstNonEmpty(spec.Locale, "en_US")
	keyboard := firstNonEmpty(spec.Keyboard, "us")
	timezone := firstNonEmpty(spec.Timezone, "UTC")
	// d-i's timezone list uses the Etc/ prefix for UTC; a bare "UTC" is not
	// accepted and makes the installer open the timezone picker.
	if timezone == "UTC" {
		timezone = "Etc/UTC"
	}
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")
	grubDev := strings.TrimSpace(spec.Disk)
	if grubDev == "" {
		grubDev = "/dev/vda"
	}

	lines := []string{
		"# Debian installer preseed generated by incus-fleet (unattended install).",
		"### Locale and keyboard",
		// The language question is asked before locale is applied; without it
		// the installer stops at the language list.
		"d-i debian-installer/language string en",
		"d-i debian-installer/locale string " + locale,
		// Without a country the installer still asks for a location before
		// the (already preseeded) timezone, blocking the unattended run.
		"d-i debian-installer/country string US",
		"d-i keyboard-configuration/xkb-keymap select " + keyboard,
		"### Network",
		"d-i netcfg/choose_interface select auto",
		"d-i netcfg/get_hostname string " + host,
		"d-i netcfg/get_domain string local",
		"d-i netcfg/hostname string " + host,
		"### Account setup",
		"d-i passwd/root-login boolean false",
		"d-i passwd/user-fullname string " + user,
		"d-i passwd/username string " + user,
		"d-i passwd/user-password password " + password,
		"d-i passwd/user-password-again password " + password,
		"d-i user-setup/allow-password-weak boolean true",
		"### Clock",
		"d-i clock-setup/utc boolean true",
		"d-i time/zone string " + timezone,
		"d-i clock-setup/ntp boolean true",
		"### Partitioning",
		// partman's one-shot auto-partition hook runs as soon as the
		// partitioner starts; on a cold virtio boot the disk can appear a few
		// seconds later, leaving the hook with no disks and dropping the
		// installer into manual mode. Give udev time to settle first.
		"d-i partman/early_command string sleep 8",
	}
	if spec.Disk != "" {
		lines = append(lines,
			"d-i partman-auto/disk string "+spec.Disk,
		)
	}
	lines = append(lines,
		"d-i partman-auto/method string regular",
		// partman-auto exposes exactly one auto-partition choice tag: "auto".
		// The first display.d question uses init_automatically_partition;
		// subsequent menus reuse automatically_partition. Seed both so the
		// initial auto-partition pass runs after disks are detected.
		"d-i partman-auto/init_automatically_partition select auto",
		"d-i partman-auto/automatically_partition select auto",
		// Explicit recipe: a BIOS boot partition (for GPT) plus an ext4 root
		// filling the rest. The built-in atomic recipe failed to produce a
		// root filesystem in the Incus VM firmware path.
		"d-i partman-auto/expert_recipe string boot-root :: 1 1 1 free method{ biosgrub } . 2000 2000 2000 linux-swap method{ swap } format{ } . 1000 1000 -1 ext4 method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ / } .",
		"d-i partman-auto/choose_recipe select boot-root",
		"d-i partman-lvm/device_remove_lvm boolean true",
		"d-i partman-md/device_remove_md boolean true",
		"d-i partman-lvm/confirm boolean true",
		"d-i partman-lvm/confirm_nooverwrite boolean true",
		"d-i partman-partitioning/confirm_write_new_label boolean true",
		"d-i partman/choose_partition select finish",
		"d-i partman/confirm boolean true",
		"d-i partman/confirm_nooverwrite boolean true",
		"### Packages",
		// Answer the mirror/proxy questions so a netinst run does not stop at
		// "Select a Debian archive mirror".
		"d-i mirror/country string manual",
		"d-i mirror/protocol string http",
		"d-i mirror/http/hostname string deb.debian.org",
		"d-i mirror/http/directory string /debian",
		"d-i mirror/http/proxy string",
		// All three CD-scan questions share the "Scan extra installation
		// media?" text; the first one is asked at critical priority.
		"d-i apt-setup/cdrom/set-first boolean false",
		"d-i apt-setup/cdrom/set-next boolean false",
		"d-i apt-setup/cdrom/set-double boolean false",
		"d-i apt-setup/non-free-firmware boolean true",
		"tasksel tasksel/first multiselect standard",
		"d-i pkgsel/include string "+strings.TrimSpace("openssh-server "+spec.Packages),
		"popularity-contest popularity-contest/participate boolean false",
		"### Boot loader",
		"d-i grub-installer/only_debian boolean true",
		"d-i grub-installer/with_other_os boolean true",
		// "default" is not a device name and makes grub-installer ask which
		// disk to use, blocking an unattended run.
		"d-i grub-installer/bootdev string "+grubDev,
		"### Finishing up",
		"d-i finish-install/keep-consoles boolean false",
		"d-i finish-install/reboot_in_progress note",
		"# Power off instead of rebooting when done: the fleet watches for the",
		"# poweroff to detach the installation media (Incus forbids -no-reboot).",
		"d-i debian-installer/exit/poweroff boolean true",
		"",
	)
	return strings.Join(lines, "\n")
}

func renderKickstart(spec AnswerSpec) string {
	locale := firstNonEmpty(spec.Locale, "en_US")
	keyboard := firstNonEmpty(spec.Keyboard, "us")
	timezone := firstNonEmpty(spec.Timezone, "UTC")
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")

	lines := []string{
		"# Kickstart generated by incus-fleet (unattended install).",
		"lang " + locale,
		"keyboard " + keyboard,
		"timezone " + timezone + " --utc",
		"network --hostname " + host,
		"rootpw --lock",
		"user --name=" + user + " --password='" + password + "' --plaintext --groups=wheel",
		"bootloader --location=mbr",
		"clearpart --all --initlabel",
		"autopart --type=plain",
		"# Power off instead of rebooting when done: the fleet watches for the",
		"# poweroff to detach the installation media.",
		"poweroff",
		"%packages",
		"@core",
		"openssh-server",
		strings.Join(strings.Fields(spec.Packages), "\n"),
		"%end",
		"%addon com_redhat_kdump --disable",
		"%end",
		"",
	}
	if spec.Disk != "" {
		lines = append([]string{"ignoredisk --only-use=" + spec.Disk}, lines...)
	}
	return strings.Join(lines, "\n")
}

func renderAutoinstall(spec AnswerSpec) (string, error) {
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")
	// Subiquity writes identity.password into /etc/shadow verbatim: a
	// plaintext value leaves the account with an invalid hash and SSH
	// password login fails. Accept an operator-supplied crypt hash as-is and
	// hash plaintext values before rendering the seed.
	if !strings.HasPrefix(password, "$") {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return "", fmt.Errorf("unattended: hash autoinstall password: %w", err)
		}
		password = string(hash)
	}
	timezone := firstNonEmpty(spec.Timezone, "UTC")
	locale := firstNonEmpty(spec.Locale, "en_US")
	keyboard := firstNonEmpty(spec.Keyboard, "us")
	packageBlock := ""
	if fields := strings.Fields(spec.Packages); len(fields) > 0 {
		packageBlock = "  packages:\n    - " + strings.Join(fields, "\n    - ") + "\n"
	}

	// Subiquity reads directives from the autoinstall: mapping in cloud-init
	// user-data; top-level keys fail schema validation and abort the install.
	return `#cloud-config
# Ubuntu autoinstall (Subiquity) generated by incus-fleet.
autoinstall:
  version: 1
  locale: ` + locale + `
  keyboard:
    layout: ` + keyboard + `
  timezone: ` + timezone + `
  # The ISO's fsimage already contains the curated server packages. Point
  # apt at no remote mirror at all: Subiquity's configure_apt step otherwise
  # stalls on unreachable archive/security endpoints before it partitions.
  apt:
    geoip: false
    preserve_sources_list: false
    primary: []
    security: []
  identity:
    hostname: ` + host + `
    username: ` + user + `
    password: "` + password + `"
  ssh:
    install-server: true
    allow-pw: true
` + packageBlock + `  shutdown: poweroff
  # Diagnostic: the QEMU serial console otherwise only shows GRUB, so dump
  # the installer's block-device view before storage is applied.
  early-commands:
    - |
      {
        echo '=== incus-fleet disk probe ==='
        lsblk -o NAME,SIZE,TYPE,MOUNTPOINT
        echo '=== /proc/partitions ==='
        cat /proc/partitions
        echo '=== virtio/scsi dmesg ==='
        dmesg | grep -iE 'virtio|vda|scsi' | tail -40
      } > /dev/ttyS0 2>&1 || true
    - |
      (while true; do
        echo '=== incus-fleet installer tail ==='
        journalctl -b --no-pager 2>/dev/null | grep -iE 'curtin|subiquity|vda|error|fail' | tail -60
        sleep 20
      done) > /dev/ttyS0 2>&1 &
  error-commands:
    - |
      {
        echo '=== incus-fleet autoinstall failure ==='
        tail -120 /var/log/installer/subiquity-debug.log 2>/dev/null
        tail -120 /var/log/installer/curtin-install.log 2>/dev/null
        journalctl -b --no-pager | tail -120
      } > /dev/ttyS0 2>&1 || true
  # Pin the install target: the VM root disk is the only virtio-blk device
  # (/dev/vda). The layout matcher otherwise risks selecting the attached
  # read-only ISO disks on some Subiquity builds, which never accepts writes.
  storage:
    config:
      - type: disk
        id: disk0
        path: /dev/vda
        ptable: gpt
        wipe: superblock-recursive
        preserve: false
        grub_device: true
      - type: partition
        id: partition-0
        device: disk0
        size: 1M
        flag: bios_grub
      - type: partition
        id: partition-1
        device: disk0
        size: -1
      - type: format
        id: format-0
        volume: partition-1
        fstype: ext4
      - type: mount
        id: mount-0
        device: format-0
        path: /
  late-commands:
    - echo 'incus-fleet unattended install' > /target/etc/motd
`, nil
}

func renderCloudConfig(spec AnswerSpec) string {
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")
	locale := firstNonEmpty(spec.Locale, "en_US")
	keyboard := firstNonEmpty(spec.Keyboard, "us")
	packageBlock := ""
	if fields := strings.Fields(spec.Packages); len(fields) > 0 {
		packageBlock = "packages:\n  - " + strings.Join(fields, "\n  - ") + "\n"
	}

	return `#cloud-config
# Generic cloud-init NoCloud seed generated by incus-fleet.
hostname: ` + host + `
manage_etc_hosts: true
timezone: ` + firstNonEmpty(spec.Timezone, "UTC") + `
locale: ` + locale + `
keyboard:
  layout: ` + keyboard + `
` + packageBlock + `ssh_pwauth: true
users:
  - name: ` + user + `
    plain_text_passwd: '` + password + `'
    lock_passwd: false
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
`
}
