package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// SelfSignedCertOptions configures GenerateSelfSignedCert. The zero value
// is usable: it produces a cert valid for 1 year with a SAN list that
// covers "localhost" and 127.0.0.1 / ::1 — enough for a local-only
// gemba serve. Callers passing --listen with a remote bind should add
// the bind IP to ExtraIPs so curl / browsers don't reject the SAN.
type SelfSignedCertOptions struct {
	// BindHost, when non-empty, is added to the SAN list. If it parses
	// as an IP it lands in IPAddresses; otherwise it lands in DNSNames.
	// Empty values are dropped.
	BindHost string
	// ExtraIPs supplements the default 127.0.0.1 / ::1 SANs.
	ExtraIPs []net.IP
	// ExtraDNS supplements the default "localhost" SAN.
	ExtraDNS []string
	// ValidFor controls cert lifetime; zero defaults to 1 year per the
	// gm-e5.3 DoD ("regenerates on missing or expired cert").
	ValidFor time.Duration
}

// GenerateSelfSignedCert produces an ephemeral ECDSA P-256 self-signed
// TLS certificate suitable for `gemba serve --tls-self-signed`. The
// returned tls.Certificate can be plugged directly into a tls.Config
// via Certificates; the SHA-256 fingerprint is provided so the
// operator can pin it (printed at startup per the gm-e5.3 DoD).
//
// ECDSA P-256 is chosen over RSA for speed: a self-signed cert is
// regenerated on every boot, so a 200ms RSA keygen would slow startup
// noticeably while contributing nothing to security (the cert is
// trusted via fingerprint pinning, not chain validation).
func GenerateSelfSignedCert(opts SelfSignedCertOptions) (tls.Certificate, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate ecdsa key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate serial: %w", err)
	}

	validFor := opts.ValidFor
	if validFor <= 0 {
		validFor = 365 * 24 * time.Hour
	}
	now := time.Now()

	dnsSet := map[string]struct{}{"localhost": {}}
	for _, d := range opts.ExtraDNS {
		if d != "" {
			dnsSet[d] = struct{}{}
		}
	}
	ipSet := map[string]net.IP{
		"127.0.0.1": net.IPv4(127, 0, 0, 1),
		"::1":       net.IPv6loopback,
	}
	for _, ip := range opts.ExtraIPs {
		if ip != nil {
			ipSet[ip.String()] = ip
		}
	}
	if opts.BindHost != "" {
		if ip := net.ParseIP(opts.BindHost); ip != nil {
			if !ip.IsUnspecified() {
				ipSet[ip.String()] = ip
			}
		} else {
			dnsSet[opts.BindHost] = struct{}{}
		}
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "gemba (self-signed)",
			Organization: []string{"gemba"},
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	for d := range dnsSet {
		tmpl.DNSNames = append(tmpl.DNSNames, d)
	}
	for _, ip := range ipSet {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("create certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("marshal private key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("assemble key pair: %w", err)
	}
	pair.Leaf = &tmpl

	fp := CertFingerprint(derBytes)
	return pair, fp, nil
}

// CertFingerprint returns the SHA-256 fingerprint of a DER-encoded
// certificate as a colon-separated uppercase hex string — the
// canonical "SHA256:AA:BB:..." form curl and browsers display when an
// operator inspects a self-signed cert.
func CertFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	hexStr := strings.ToUpper(hex.EncodeToString(sum[:]))
	var b strings.Builder
	b.Grow(len(hexStr) + len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexStr[i : i+2])
	}
	return b.String()
}

// CheckCertKeyPermissions warns if the operator-supplied key file is
// world- or group-readable. Returns the permission bits and a
// non-empty warning string when the file is more permissive than 0600;
// callers log the warning rather than abort so an operator running
// under a deliberately-relaxed umask can still boot. Missing files
// return an error so the caller fails fast.
func CheckCertKeyPermissions(path string) (os.FileMode, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		return mode, fmt.Sprintf(
			"TLS key file %s has permissions %#o; "+
				"recommend chmod 600 to keep the key private",
			path, mode), nil
	}
	return mode, "", nil
}
