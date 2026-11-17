// Package bootstrap defines the signed Incus bootstrap contract shared by the
// center and the host helper.
package bootstrap

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// Spec is a declarative Incus install/init/join request. The Signature field
// is excluded from the signed canonical JSON.
type Spec struct {
	Version       int      `json:"version"`
	Mode          string   `json:"mode"` // join or init
	Cluster       string   `json:"cluster"`
	Member        string   `json:"member"`
	Address       string   `json:"address"`
	StorageSource string   `json:"storage_source,omitempty"`
	NetworkCIDR   string   `json:"network_cidr,omitempty"`
	Preseed       string   `json:"preseed"`
	InstallMode   string   `json:"install_mode"` // existing or package
	RepoURL       string   `json:"repo_url,omitempty"`
	RepoKeyURL    string   `json:"repo_key_url,omitempty"`
	VersionPin    string   `json:"version_pin,omitempty"`
	Packages      []string `json:"packages,omitempty"`
	TaskID        string   `json:"task_id"`
	Nonce         string   `json:"nonce"`
	ExpiresAt     int64    `json:"expires_at"`
	Signature     string   `json:"signature,omitempty"`
}

// Canonical returns the bytes that are signed.
func Canonical(spec *Spec) ([]byte, error) {
	clone := *spec
	clone.Signature = ""
	return json.Marshal(clone)
}

// Sign signs the canonical spec with an ECDSA, RSA, or Ed25519 key.
func Sign(signer crypto.Signer, spec *Spec) error {
	data, err := Canonical(spec)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)

	var signature []byte
	switch signer.Public().(type) {
	case ed25519.PublicKey:
		signature, err = signer.Sign(rand.Reader, data, crypto.Hash(0))
	default:
		signature, err = signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	}
	if err != nil {
		return fmt.Errorf("bootstrap: sign spec: %w", err)
	}
	spec.Signature = base64.StdEncoding.EncodeToString(signature)

	return nil
}

// Verify checks a spec signature against a PEM certificate.
func Verify(certPEM []byte, spec *Spec) error {
	if spec.Signature == "" {
		return errors.New("bootstrap: missing signature")
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("bootstrap: CA certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("bootstrap: parse CA certificate: %w", err)
	}
	if !cert.IsCA {
		return errors.New("bootstrap: signing certificate is not a CA")
	}
	data, err := Canonical(spec)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(spec.Signature)
	if err != nil {
		return fmt.Errorf("bootstrap: decode signature: %w", err)
	}
	digest := sha256.Sum256(data)

	switch public := cert.PublicKey.(type) {
	case ed25519.PublicKey:
		if !ed25519.Verify(public, data, signature) {
			return errors.New("bootstrap: invalid signature")
		}
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(public, digest[:], signature) {
			return errors.New("bootstrap: invalid signature")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature); err != nil {
			return errors.New("bootstrap: invalid signature")
		}
	default:
		return errors.New("bootstrap: unsupported CA public key")
	}

	return nil
}

// Validate checks transport-independent constraints before execution.
func (s *Spec) Validate(now time.Time) error {
	if s.Version != 1 {
		return fmt.Errorf("bootstrap: unsupported spec version %d", s.Version)
	}
	if s.Mode != "join" && s.Mode != "init" {
		return fmt.Errorf("bootstrap: invalid mode %q", s.Mode)
	}
	if s.Cluster == "" || s.Member == "" || s.Address == "" || s.TaskID == "" || s.Nonce == "" {
		return errors.New("bootstrap: cluster, member, address, task_id, and nonce are required")
	}
	if s.ExpiresAt <= now.Unix() {
		return errors.New("bootstrap: spec expired")
	}
	if s.Preseed == "" {
		return errors.New("bootstrap: preseed is required")
	}

	return nil
}
