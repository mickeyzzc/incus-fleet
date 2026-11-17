package bootstrap

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	spec := &Spec{
		Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "cluster:\n  enabled: true\n",
		TaskID: "task-1", Nonce: "nonce", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := Sign(key, spec); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := Verify(certPEM, spec); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := spec.Validate(time.Now()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	spec.Member = "node-b"
	if err := Verify(certPEM, spec); err == nil {
		t.Fatal("Verify(tampered) expected error")
	}
}

func TestValidateRejectsExpiredAndIncomplete(t *testing.T) {
	spec := &Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	if err := spec.Validate(time.Now()); err == nil {
		t.Fatal("Validate(expired) expected error")
	}
	spec.ExpiresAt = time.Now().Add(time.Hour).Unix()
	spec.Preseed = ""
	if err := spec.Validate(time.Now()); err == nil {
		t.Fatal("Validate(no preseed) expected error")
	}
}

func TestVerifyErrors(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "ca2"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	spec := &Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := Verify(certPEM, spec); err == nil {
		t.Fatal("Verify(unsigned) expected error")
	}
	spec.Signature = "not-base64"
	if err := Verify(certPEM, spec); err == nil {
		t.Fatal("Verify(bad base64) expected error")
	}
	if err := Verify([]byte("not-pem"), spec); err == nil {
		t.Fatal("Verify(bad cert) expected error")
	}
	if err := Sign(key, spec); err != nil {
		t.Fatal(err)
	}
	if err := Verify(certPEM, spec); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherTmpl := *tmpl
	otherTmpl.SerialNumber = big.NewInt(3)
	otherDER, _ := x509.CreateCertificate(rand.Reader, &otherTmpl, &otherTmpl, &other.PublicKey, other)
	otherPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: otherDER})
	if err := Verify(otherPEM, spec); err == nil {
		t.Fatal("Verify(wrong CA) expected error")
	}
}

func TestValidateRejectsBadFields(t *testing.T) {
	now := time.Now()
	base := Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: now.Add(time.Hour).Unix()}
	cases := []func(*Spec){
		func(s *Spec) { s.Version = 2 },
		func(s *Spec) { s.Mode = "magic" },
		func(s *Spec) { s.Cluster = "" },
		func(s *Spec) { s.Member = "" },
		func(s *Spec) { s.Address = "" },
		func(s *Spec) { s.TaskID = "" },
		func(s *Spec) { s.Nonce = "" },
	}
	for i, mutate := range cases {
		spec := base
		mutate(&spec)
		if err := spec.Validate(now); err == nil {
			t.Fatalf("Validate(case %d) expected error", i)
		}
	}
	if err := base.Validate(now); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
}

func TestSignEd25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "ed-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	spec := &Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := Sign(priv, spec); err != nil {
		t.Fatal(err)
	}
	if err := Verify(certPEM, spec); err != nil {
		t.Fatalf("Verify(ed25519) error = %v", err)
	}
}

func TestSignRSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(5), Subject: pkix.Name{CommonName: "rsa-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	spec := &Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := Sign(key, spec); err != nil {
		t.Fatal(err)
	}
	if err := Verify(certPEM, spec); err != nil {
		t.Fatalf("Verify(rsa) error = %v", err)
	}
}

func TestVerifyRejectsLeafSigner(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(6), Subject: pkix.Name{CommonName: "leaf"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	spec := &Spec{Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "x", TaskID: "t", Nonce: "n",
		ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := Sign(key, spec); err != nil {
		t.Fatal(err)
	}
	if err := Verify(certPEM, spec); err == nil {
		t.Fatal("Verify(leaf) expected error")
	}
}
