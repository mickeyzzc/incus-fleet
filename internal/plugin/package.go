package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadPackageManifest reads and validates <dir>/<id>/manifest.yaml. It is the
// shared loader used by the install API and by startup recovery of installed
// community plugins.
func LoadPackageManifest(dir, id string) (*Manifest, []byte, error) {
	dir = strings.TrimSpace(dir)
	id = strings.TrimSpace(id)
	if dir == "" || !pluginIDRE.MatchString(id) {
		return nil, nil, fmt.Errorf("%w: invalid plugin package id %q", ErrInvalidManifest, id)
	}
	path := filepath.Join(dir, id, "manifest.yaml") //nolint:gosec // id is a validated DNS label
	raw, err := os.ReadFile(path)                   //nolint:gosec // operator-provided plugin directory, id is validated
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("plugin package %s: %w", id, err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	if m.ID != id {
		return nil, nil, fmt.Errorf("%w: manifest id %q does not match package %q", ErrInvalidManifest, m.ID, id)
	}

	return m, raw, nil
}

// VerifyPackageSignature checks the manifest.yaml.sig sidecar of a community
// package. Official packages need no sidecar; without a signing key there is no
// trust anchor, so community packages are accepted only for local development.
func VerifyPackageSignature(m *Manifest, dir, id string, raw, key []byte) bool {
	if len(key) == 0 || m.Tier == TierOfficial {
		return true
	}
	sig, err := os.ReadFile(filepath.Join(dir, id, "manifest.yaml.sig")) //nolint:gosec // id is a validated DNS label
	if err != nil {
		return false
	}

	return VerifySignature(raw, string(sig), key)
}
