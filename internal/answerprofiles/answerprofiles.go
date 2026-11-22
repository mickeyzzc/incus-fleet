// Package answerprofiles keeps reusable unattended-install answer
// configurations in the fleet database and resolves them at launch time.
package answerprofiles

import (
	"context"
	"fmt"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/unattended"
)

// AnswerProfile is one reusable answer configuration.
type AnswerProfile = store.AnswerProfile

// Service exposes answer configuration CRUD and default resolution.
type Service struct {
	store store.Store
}

// New builds the answer profile service.
func New(st store.Store) *Service {
	return &Service{store: st}
}

// validate rejects invalid configurations before they reach storage. A
// profile either supplies the built-in template fields or a complete custom
// answer file; username/hostname format follows the same rules as a launch.
func validate(p *AnswerProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: answer profile name is required", instances.ErrInvalidRequest)
	}
	if err := unattended.Validate(unattended.Profile(p.Profile)); err != nil {
		return fmt.Errorf("%w: %w", instances.ErrInvalidRequest, err)
	}
	spec := unattended.AnswerSpec{
		Hostname: p.Hostname, Username: p.Username, Password: p.Password,
		Timezone: p.Timezone, Disk: p.Disk, Locale: p.Locale, Keyboard: p.Keyboard,
	}
	if strings.TrimSpace(p.AnswerFile) == "" {
		if err := unattended.ValidateAnswerSpec(spec); err != nil {
			return fmt.Errorf("%w: %w", instances.ErrInvalidRequest, err)
		}
		if strings.TrimSpace(p.Username) == "" {
			return fmt.Errorf("%w: username is required for template profiles", instances.ErrInvalidRequest)
		}
	} else if strings.ContainsRune(p.AnswerFile, '\x00') {
		return fmt.Errorf("%w: answer file must be text", instances.ErrInvalidRequest)
	}

	return nil
}

// Create validates and stores a new answer configuration.
func (s *Service) Create(ctx context.Context, p *AnswerProfile) error {
	if err := validate(p); err != nil {
		return err
	}
	p.ID = store.NewID()
	p.Name = strings.TrimSpace(p.Name)

	return s.store.CreateAnswerProfile(ctx, p)
}

// List returns every answer configuration.
func (s *Service) List(ctx context.Context) ([]*AnswerProfile, error) {
	return s.store.ListAnswerProfiles(ctx)
}

// Get loads one configuration by id.
func (s *Service) Get(ctx context.Context, id string) (*AnswerProfile, error) {
	return s.store.GetAnswerProfile(ctx, id)
}

// Update validates and rewrites an existing configuration.
func (s *Service) Update(ctx context.Context, p *AnswerProfile) error {
	if err := validate(p); err != nil {
		return err
	}
	p.Name = strings.TrimSpace(p.Name)

	return s.store.UpdateAnswerProfile(ctx, p)
}

// Delete removes one configuration.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.DeleteAnswerProfile(ctx, id)
}

// Resolve finds the configuration a launch should use. Explicit IDs win;
// otherwise the installer profile's default is used. ErrNotFound means the
// caller should fall back to the built-in template.
func (s *Service) Resolve(ctx context.Context, id, profile string) (*AnswerProfile, error) {
	if strings.TrimSpace(id) != "" {
		return s.store.GetAnswerProfile(ctx, id)
	}

	return s.store.GetDefaultAnswerProfile(ctx, profile)
}

// ToSpec converts a stored configuration into the launch answer spec.
func ToSpec(p *AnswerProfile) unattended.AnswerSpec {
	return unattended.AnswerSpec{
		Hostname: p.Hostname, Username: p.Username, Password: p.Password,
		Timezone: p.Timezone, Disk: p.Disk, Locale: p.Locale, Keyboard: p.Keyboard,
	}
}
