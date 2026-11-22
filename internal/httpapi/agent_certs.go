package httpapi

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"time"
)

// CertificateIssuer signs a new client certificate for an existing agent key.
type CertificateIssuer interface {
	IssueCertificate(agentID, cluster, member string, publicKey crypto.PublicKey, ttl time.Duration) ([]byte, string, time.Time, error)
}

func (a *API) rotateAgentCertificate(w http.ResponseWriter, r *http.Request) {
	if a.certIssuer == nil || a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent certificate authority is not enabled")
		return
	}
	var req struct {
		PublicKey string `json:"public_key"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	block, _ := pem.Decode([]byte(req.PublicKey))
	if block == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "public_key must be PKIX PEM")
		return
	}
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "public_key must be PKIX PEM")
		return
	}
	agent, err := a.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	certPEM, fingerprint, expires, err := a.certIssuer.IssueCertificate(agent.ID, agent.Cluster, agent.Member, publicKey, 90*24*time.Hour)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	agent.CertFingerprint = fingerprint
	agent.CertNotAfter = expires
	agent.UpdatedAt = time.Now().UTC()
	if err := a.store.UpsertAgent(r.Context(), agent); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"certificate": string(certPEM), "fingerprint": fingerprint, "expires_at": expires,
	})
}

func (a *API) revokeAgent(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent registry is not enabled")
		return
	}
	if err := a.store.RevokeAgent(r.Context(), r.PathValue("id")); err != nil {
		a.serviceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
