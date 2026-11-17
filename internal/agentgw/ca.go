// Package agentgw implements the center-side enrollment API and mTLS
// WebSocket gateway for host agents.
package agentgw

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
)

const defaultAgentCertTTL = 90 * 24 * time.Hour

// CA signs agent client certificates.
type CA struct {
	cert    *x509.Certificate
	key     crypto.Signer
	certPEM []byte
	pool    *x509.CertPool
}

// LoadCA reads a PEM certificate and private key from disk.
func LoadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // operator-supplied CA path
	if err != nil {
		return nil, fmt.Errorf("agentgw: read CA certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // operator-supplied CA key path
	if err != nil {
		return nil, fmt.Errorf("agentgw: read CA key: %w", err)
	}

	return ParseCA(certPEM, keyPEM)
}

// ParseCA parses PEM certificate and key bytes.
func ParseCA(certPEM, keyPEM []byte) (*CA, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("agentgw: parse CA key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("agentgw: CA certificate is empty")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("agentgw: parse CA certificate: %w", err)
	}
	if !cert.IsCA {
		return nil, errors.New("agentgw: certificate is not a CA")
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("agentgw: CA private key cannot sign")
	}

	return &CA{cert: cert, key: key, certPEM: certPEM, pool: poolFor(cert)}, nil
}

// GenerateCA creates a self-signed CA for tests and small deployments.
func GenerateCA(commonName string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalKey(key)})

	return ParseCA(certPEM, keyPEM)
}

// SignBootstrap signs a bootstrap spec with the CA key.
func (c *CA) SignBootstrap(spec *bootstrap.Spec) error {
	return bootstrap.Sign(c.key, spec)
}

// CertPEM returns the CA certificate in PEM form.
func (c *CA) CertPEM() []byte { return append([]byte(nil), c.certPEM...) }

// KeyPEM returns the CA private key in PKCS#8 PEM form. Callers must keep it
// private; it is exported for installers and tests.
func (c *CA) KeyPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalKey(c.key)})
}

// Pool returns a copy of the CA trust pool.
func (c *CA) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// IssueAgent signs an Ed25519/ECDSA/RSA public key for one agent. The CN is
// the agent ID; the cluster and member are carried in the subject.
func (c *CA) IssueAgent(agentID, cluster, member string, publicKey crypto.PublicKey, ttl time.Duration) ([]byte, string, time.Time, error) {
	if ttl <= 0 {
		ttl = defaultAgentCertTTL
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: agentID, Organization: []string{cluster}, OrganizationalUnit: []string{member}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, publicKey, c.key)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("agentgw: issue agent certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", time.Time{}, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Fingerprint(cert), cert.NotAfter, nil
}

// IssueServer signs a server certificate for tests and internal listeners.
func (c *CA) IssueServer(hosts []string, ttl time.Duration) (tls.Certificate, error) {
	if ttl <= 0 {
		ttl = 365 * 24 * time.Hour
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "incus-fleet-agent-gateway"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// Fingerprint returns the SHA-256 fingerprint of a certificate.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

func poolFor(cert *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	if serial.Sign() == 0 {
		return big.NewInt(1), nil
	}

	return serial, nil
}

func mustMarshalKey(key crypto.Signer) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}

	return der
}
