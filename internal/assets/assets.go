// Package assets keeps shared operations content (SSH public keys and
// shell scripts) in the fleet database and applies them to many instances
// at once.
package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// Service exposes the asset library and the batch operations built on it.
type Service struct {
	store     store.Store
	instances *instances.Service
}

// New builds the asset service.
func New(st store.Store, instanceService *instances.Service) *Service {
	return &Service{store: st, instances: instanceService}
}

// Keystone is one reusable SSH public key.
type Keystone = store.Keystone

// Script is one reusable shell script.
type Script = store.Script

// validateKeyMaterial accepts one or more single-line public keys.
func validateKeyMaterial(publicKey string) error {
	trimmed := strings.TrimSpace(publicKey)
	if trimmed == "" {
		return fmt.Errorf("%w: public key is required", instances.ErrInvalidRequest)
	}
	if strings.Contains(trimmed, "\r") {
		return fmt.Errorf("%w: public key must use unix line endings", instances.ErrInvalidRequest)
	}

	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " ") {
			return fmt.Errorf("%w: each key line must look like 'ssh-ed25519 AAAA... comment'", instances.ErrInvalidRequest)
		}
	}

	return nil
}

// normalizeKeyMaterial trims every key line so stored material is clean.
func normalizeKeyMaterial(publicKey string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(publicKey), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// newKeyID derives a stable id from the key material: identical keys are
// stored once under the same id.
func newKeyID(publicKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(publicKey)))

	return "key-" + hex.EncodeToString(sum[:8])
}

// CreateKey validates and stores one public key.
func (s *Service) CreateKey(ctx context.Context, key *Keystone) error {
	if strings.TrimSpace(key.Name) == "" {
		return fmt.Errorf("%w: key name is required", instances.ErrInvalidRequest)
	}
	if err := validateKeyMaterial(key.PublicKey); err != nil {
		return err
	}

	key.PublicKey = normalizeKeyMaterial(key.PublicKey)
	key.ID = newKeyID(key.PublicKey)
	key.CreatedAt = time.Now().UTC()

	return s.store.CreateKeystone(ctx, key)
}

// Keys lists every stored key.
func (s *Service) Keys(ctx context.Context) ([]*Keystone, error) {
	return s.store.ListKeystones(ctx)
}

// Key loads one key.
func (s *Service) Key(ctx context.Context, id string) (*Keystone, error) {
	return s.store.GetKeystone(ctx, id)
}

// UpdateKey rewrites an existing key entry.
func (s *Service) UpdateKey(ctx context.Context, key *Keystone) error {
	if err := validateKeyMaterial(key.PublicKey); err != nil {
		return err
	}
	key.PublicKey = normalizeKeyMaterial(key.PublicKey)

	return s.store.UpdateKeystone(ctx, key)
}

// DeleteKey removes one key entry.
func (s *Service) DeleteKey(ctx context.Context, id string) error {
	return s.store.DeleteKeystone(ctx, id)
}

// CreateScript validates and stores one script.
func (s *Service) CreateScript(ctx context.Context, script *Script) error {
	if strings.TrimSpace(script.Name) == "" {
		return fmt.Errorf("%w: script name is required", instances.ErrInvalidRequest)
	}
	if strings.TrimSpace(script.Body) == "" {
		return fmt.Errorf("%w: script body is required", instances.ErrInvalidRequest)
	}

	script.ID = "script-" + newKeyID(script.Name + script.Body)[4:]
	script.CreatedAt = time.Now().UTC()
	script.UpdatedAt = script.CreatedAt

	return s.store.CreateScript(ctx, script)
}

// Scripts lists every stored script.
func (s *Service) Scripts(ctx context.Context) ([]*Script, error) {
	return s.store.ListScripts(ctx)
}

// Script loads one script.
func (s *Service) Script(ctx context.Context, id string) (*Script, error) {
	return s.store.GetScript(ctx, id)
}

// UpdateScript rewrites an existing script.
func (s *Service) UpdateScript(ctx context.Context, script *Script) error {
	if strings.TrimSpace(script.Body) == "" {
		return fmt.Errorf("%w: script body is required", instances.ErrInvalidRequest)
	}

	return s.store.UpdateScript(ctx, script)
}

// DeleteScript removes one script.
func (s *Service) DeleteScript(ctx context.Context, id string) error {
	return s.store.DeleteScript(ctx, id)
}
