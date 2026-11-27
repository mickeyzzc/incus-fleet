package plugin

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var configKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// RenderTemplates fills manifest-declared install settings with operator
// overrides, validating each value against its declared type. Unknown keys are
// rejected so a typo cannot silently drop a setting.
func RenderTemplates(m Manifest, values map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(m.Install.Templates))
	for i := range m.Install.Templates {
		t := m.Install.Templates[i]
		value := t.Default
		if override, ok := values[t.Name]; ok && strings.TrimSpace(override) != "" {
			value = strings.TrimSpace(override)
		}
		if err := validateTemplateValue(t, value); err != nil {
			return nil, err
		}
		out[t.Name] = value
	}
	for key := range values {
		found := false
		for i := range m.Install.Templates {
			if m.Install.Templates[i].Name == key {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: unknown install template %q", ErrInvalidManifest, key)
		}
	}

	return out, nil
}

func validateTemplateValue(t Template, value string) error {
	switch t.Type {
	case TemplateChoice:
		for _, option := range t.Options {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("%w: template %q value %q is not an option", ErrInvalidManifest, t.Name, value)
	case TemplatePort:
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%w: template %q value %q is not a valid port", ErrInvalidManifest, t.Name, value)
		}
	case TemplateBool:
		if value != "true" && value != "false" {
			return fmt.Errorf("%w: template %q value %q is not a boolean", ErrInvalidManifest, t.Name, value)
		}
	case TemplateText:
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("%w: template %q value must be a single line", ErrInvalidManifest, t.Name)
		}
	default:
		return fmt.Errorf("%w: template %q has unsupported type %q", ErrInvalidManifest, t.Name, t.Type)
	}

	return nil
}

// ConfigScript renders the resolved install values into a deterministic shell
// script that writes an env file inside the plugin instance. Sorted keys keep
// the generated script stable across installs and test runs.
func ConfigScript(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out strings.Builder
	out.WriteString("#!/bin/sh\nset -eu\nmkdir -p /etc/fleet-plugin\n: > /etc/fleet-plugin/config.env\n")
	for _, key := range keys {
		if !configKeyRE.MatchString(key) {
			continue
		}
		// Single quotes keep shell metacharacters literal; an embedded quote is
		// closed, escaped, and reopened in the usual POSIX way.
		value := strings.ReplaceAll(values[key], "'", `'\''`)
		fmt.Fprintf(&out, "echo '%s' >> /etc/fleet-plugin/config.env\n", key+"="+value)
	}

	return out.String()
}

// DataDiskScript returns the format/mount/fstab step for an allocated data
// volume. The device is a fixed Incus custom-volume path and the mount point is
// validated by Manifest.Validate before this function is called.
func DataDiskScript(m Manifest, sizeGB int) string {
	mount := strings.TrimSpace(m.Install.DataDisk.MountPoint)
	if mount == "" {
		mount = defaultMountPoint
	}
	fs := m.Install.DataDisk.Filesystem
	if fs == "" {
		fs = FSExt4
	}

	var out strings.Builder
	fmt.Fprintf(&out, "#!/bin/sh\nset -eu\n# data volume %d GiB, %s\n", sizeGB, fs)
	out.WriteString("device=/dev/disk/by-id/fleet-plugin-data\n")
	fmt.Fprintf(&out, "mkdir -p %q\n", mount)
	fmt.Fprintf(&out, "if ! blkid \"$device\" >/dev/null 2>&1; then mkfs.%s \"$device\"; fi\n", fs)
	fmt.Fprintf(&out, "mount \"$device\" %q\n", mount)
	fmt.Fprintf(&out, "grep -q ' %s ' /etc/fstab || printf '%%s\\n' \"$device %s %s defaults,nofail 0 2\" >> /etc/fstab\n", mount, mount, fs)

	return out.String()
}
