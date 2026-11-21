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
		return renderAutoinstall(spec), nil
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
	default:
		return content, 0, fmt.Errorf("unattended: profile %q does not patch boot configuration", string(p))
	}

	changed := 0
	alreadyPatched := 0
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, "preseed") || strings.Contains(line, "inst.ks") || strings.Contains(line, "auto=true") {
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
func RenderSeedFiles(p Profile, spec AnswerSpec) ([]SeedFile, error) {
	if NeedsRemaster(p) {
		return nil, nil
	}

	content, err := RenderAnswerFile(p, spec)
	if err != nil {
		return nil, err
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
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")

	lines := []string{
		"# Debian installer preseed generated by incus-fleet (unattended install).",
		"### Locale and keyboard",
		"d-i debian-installer/locale string " + locale,
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
	}
	if spec.Disk != "" {
		lines = append(lines,
			"d-i partman-auto/disk string "+spec.Disk,
		)
	}
	lines = append(lines,
		"d-i partman-auto/method string regular",
		"# The atomic recipe uses the whole disk with a single partition.",
		"d-i partman-auto/choose_recipe select atomic",
		"d-i partman-partitioning/confirm_write_new_label boolean true",
		"d-i partman/choose_partition select finish",
		"d-i partman/confirm boolean true",
		"d-i partman/confirm_nooverwrite boolean true",
		"### Packages",
		"d-i apt-setup/non-free-firmware boolean true",
		"tasksel tasksel/first multiselect standard",
		"d-i pkgsel/include string openssh-server",
		"popularity-contest popularity-contest/participate boolean false",
		"### Boot loader",
		"d-i grub-installer/only_debian boolean true",
		"d-i grub-installer/bootdev string default",
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

func renderAutoinstall(spec AnswerSpec) string {
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")
	timezone := firstNonEmpty(spec.Timezone, "UTC")

	return `#cloud-config
# Ubuntu autoinstall (Subiquity) generated by incus-fleet.
version: 1
locale: en_US
timezone: ` + timezone + `
identity:
  hostname: ` + host + `
  username: ` + user + `
  password: "` + password + `"
ssh:
  install-server: true
  allow-pw: true
shutdown: poweroff
storage:
  layout:
    name: direct
late-commands:
  - echo 'incus-fleet unattended install' > /target/etc/motd
`
}

func renderCloudConfig(spec AnswerSpec) string {
	host := firstNonEmpty(spec.Hostname, "fleet-unattended")
	user := firstNonEmpty(spec.Username, "fleet")
	password := firstNonEmpty(spec.Password, "fleet")

	return `#cloud-config
# Generic cloud-init NoCloud seed generated by incus-fleet.
hostname: ` + host + `
manage_etc_hosts: true
timezone: ` + firstNonEmpty(spec.Timezone, "UTC") + `
ssh_pwauth: true
users:
  - name: ` + user + `
    plain_text_passwd: '` + password + `'
    lock_passwd: false
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
`
}
