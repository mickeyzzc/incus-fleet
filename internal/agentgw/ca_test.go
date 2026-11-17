package agentgw

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func agentKeyPair(t *testing.T) (publicPEM, privatePEM []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
}

func TestCAIssueAgent(t *testing.T) {
	ca, err := GenerateCA("incus-fleet-test-ca")
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}
	pubPEM, _ := agentKeyPair(t)
	block, _ := pem.Decode(pubPEM)
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	certPEM, fingerprint, expires, err := ca.IssueAgent("agent-1", "c1", "node-a", pub, time.Hour)
	if err != nil {
		t.Fatalf("IssueAgent() error = %v", err)
	}
	if fingerprint == "" || !expires.After(time.Now()) {
		t.Fatalf("IssueAgent() fingerprint=%q expires=%v", fingerprint, expires)
	}
	parsed, err := parseCertificate(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject.CommonName != "agent-1" || parsed.Subject.Organization[0] != "c1" ||
		parsed.Subject.OrganizationalUnit[0] != "node-a" {
		t.Fatalf("certificate subject = %#v", parsed.Subject)
	}
	if fingerprint != Fingerprint(parsed) {
		t.Fatalf("fingerprint mismatch: %q vs %q", fingerprint, Fingerprint(parsed))
	}
	if _, err := ca.IssueServer([]string{"127.0.0.1"}, time.Hour); err != nil {
		t.Fatalf("IssueServer() error = %v", err)
	}
}

func parseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	return x509.ParseCertificate(block.Bytes)
}

func TestLoadCAFromFiles(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if err := os.WriteFile(certPath, ca.CertPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalKey(ca.key)})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA() error = %v", err)
	}
	if len(loaded.CertPEM()) == 0 || loaded.Pool() == nil {
		t.Fatal("LoadCA() returned empty CA")
	}
	if _, err := LoadCA(filepath.Join(dir, "missing"), keyPath); err == nil {
		t.Fatal("LoadCA(missing) expected error")
	}
}

func TestParseCARejectsNonCA(t *testing.T) {
	ca, err := GenerateCA("signer")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ca.IssueServer([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate[0]})
	keyDER, err := x509.MarshalPKCS8PrivateKey(server.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if _, err := ParseCA(certPEM, keyPEM); err == nil {
		t.Fatal("ParseCA(leaf) expected error")
	}
}

func TestParseCAErrors(t *testing.T) {
	if _, err := ParseCA([]byte("bad"), []byte("bad")); err == nil {
		t.Fatal("ParseCA(bad) expected error")
	}
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCA(ca.CertPEM(), []byte("bad-key")); err == nil {
		t.Fatal("ParseCA(bad key) expected error")
	}
	if _, _, _, err := ca.IssueAgent("agent-1", "c1", "node-a", ca.key.Public(), 0); err != nil {
		t.Fatalf("IssueAgent(default TTL) error = %v", err)
	}
	if _, err := ca.IssueServer([]string{"localhost", "127.0.0.1"}, 0); err != nil {
		t.Fatalf("IssueServer(default TTL) error = %v", err)
	}
}

func TestLoadCAMissingKey(t *testing.T) {
	ca, err := GenerateCA("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(certPath, ca.CertPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCA(certPath, filepath.Join(dir, "missing.key")); err == nil {
		t.Fatal("LoadCA(missing key) expected error")
	}
}
