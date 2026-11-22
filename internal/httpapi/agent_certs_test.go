package httpapi

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type fakeCertIssuer struct {
	err error
}

func (f *fakeCertIssuer) IssueCertificate(string, string, string, crypto.PublicKey, time.Duration) ([]byte, string, time.Time, error) {
	if f.err != nil {
		return nil, "", time.Time{}, f.err
	}
	return []byte("cert"), "new-fingerprint", time.Now().Add(time.Hour), nil
}

func publicKeyPEM(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestAgentCertificateRotationAndRevocation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	agent := &store.Agent{ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := f.store.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Auth: f.auth, Version: "test",
		CertIssuer: &fakeCertIssuer{},
	}).Handler()
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/certificate", map[string]string{"public_key": publicKeyPEM(t)}, cookie, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "new-fingerprint") {
		t.Fatalf("rotate = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, _ := f.store.GetAgent(context.Background(), "agent-1")
	if updated.CertFingerprint != "new-fingerprint" {
		t.Fatalf("fingerprint = %q", updated.CertFingerprint)
	}

	bad := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/certificate", map[string]string{"public_key": "bad"}, cookie, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad key = %d", bad.Code)
	}
	revoke := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/revoke", nil, cookie, "")
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d body=%s", revoke.Code, revoke.Body.String())
	}
	updated, _ = f.store.GetAgent(context.Background(), "agent-1")
	if updated.Status != store.AgentRevoked {
		t.Fatalf("status = %q", updated.Status)
	}

	f.handler = New(Deps{Auth: f.auth, Store: f.store, Version: "test"}).Handler()
	disabled := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/certificate", map[string]string{"public_key": publicKeyPEM(t)}, cookie, "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d", disabled.Code)
	}

	f.handler = New(Deps{Auth: f.auth, Store: f.store, CertIssuer: &fakeCertIssuer{err: errors.New("no")}, Version: "test"}).Handler()
	failed := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/certificate", map[string]string{"public_key": publicKeyPEM(t)}, cookie, "")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("issuer error = %d", failed.Code)
	}
}

func TestRevokeAgentBranches(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	missing := f.request(t, http.MethodPost, "/api/v1/agents/missing/revoke", nil, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing revoke = %d", missing.Code)
	}
	f.handler = New(Deps{Auth: f.auth, Version: "test"}).Handler()
	disabled := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/revoke", nil, cookie, "")
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled revoke = %d", disabled.Code)
	}
}

func TestRotateAgentValidation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{Auth: f.auth, Store: f.store, CertIssuer: &fakeCertIssuer{}, Version: "test"}).Handler()
	cookie := f.login(t)
	bad := f.request(t, http.MethodPost, "/api/v1/agents/agent-1/certificate", "{bad", cookie, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", bad.Code)
	}
	missing := f.request(t, http.MethodPost, "/api/v1/agents/missing/certificate", map[string]string{"public_key": publicKeyPEM(t)}, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing agent = %d", missing.Code)
	}
}
