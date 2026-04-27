package auth

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGenerateSelfSignedCert_DefaultOpts ensures the zero-value options
// path produces a usable cert with the expected SAN coverage and a
// non-empty fingerprint. This is the gm-e5.3 "self-signed mode works"
// gate: any regression here means `gemba serve --tls-self-signed`
// can't boot.
func TestGenerateSelfSignedCert_DefaultOpts(t *testing.T) {
	cert, fp, err := GenerateSelfSignedCert(SelfSignedCertOptions{})
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("cert.Certificate empty")
	}
	if cert.PrivateKey == nil {
		t.Fatal("cert.PrivateKey nil")
	}
	if fp == "" {
		t.Fatal("fingerprint empty")
	}
	if !strings.Contains(fp, ":") {
		t.Errorf("fingerprint should be colon-separated; got %q", fp)
	}

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// Default SANs MUST cover localhost + loopback so curl --resolve
	// and direct 127.0.0.1 hits both validate against the SAN list.
	wantDNS := map[string]bool{"localhost": false}
	for _, d := range parsed.DNSNames {
		if _, ok := wantDNS[d]; ok {
			wantDNS[d] = true
		}
	}
	for d, found := range wantDNS {
		if !found {
			t.Errorf("missing DNS SAN: %q (got %v)", d, parsed.DNSNames)
		}
	}

	wantIPs := map[string]bool{"127.0.0.1": false, "::1": false}
	for _, ip := range parsed.IPAddresses {
		if _, ok := wantIPs[ip.String()]; ok {
			wantIPs[ip.String()] = true
		}
	}
	for ip, found := range wantIPs {
		if !found {
			t.Errorf("missing IP SAN: %q (got %v)", ip, parsed.IPAddresses)
		}
	}

	if parsed.NotAfter.Before(time.Now().Add(300 * 24 * time.Hour)) {
		t.Errorf("cert expires too soon: %v", parsed.NotAfter)
	}
}

// TestGenerateSelfSignedCert_BindHostIPv4 covers the path the operator
// hits with --listen 192.168.1.5: the bind IP must land in the SAN
// list so curl https://192.168.1.5 doesn't reject the cert with a SAN
// mismatch.
func TestGenerateSelfSignedCert_BindHostIPv4(t *testing.T) {
	cert, _, err := GenerateSelfSignedCert(SelfSignedCertOptions{
		BindHost: "192.168.1.5",
	})
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	found := false
	for _, ip := range parsed.IPAddresses {
		if ip.Equal(net.ParseIP("192.168.1.5")) {
			found = true
		}
	}
	if !found {
		t.Errorf("bind host IP missing from SANs: %v", parsed.IPAddresses)
	}
}

// TestGenerateSelfSignedCert_BindHostName covers the hostname path
// (e.g. --listen gemba.example.com): non-IP bind hosts must land in
// DNSNames not IPAddresses.
func TestGenerateSelfSignedCert_BindHostName(t *testing.T) {
	cert, _, err := GenerateSelfSignedCert(SelfSignedCertOptions{
		BindHost: "gemba.example.com",
	})
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	found := false
	for _, d := range parsed.DNSNames {
		if d == "gemba.example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("bind hostname missing from DNS SANs: %v", parsed.DNSNames)
	}
}

// TestCertFingerprint_Format pins the colon-separated uppercase form
// so the operator-facing banner stays stable. Hex output without
// colons would still verify but breaks copy-paste into pinning tools
// like `openssl x509 -fingerprint`.
func TestCertFingerprint_Format(t *testing.T) {
	fp := CertFingerprint([]byte("hello"))
	// SHA-256 of "hello" in uppercase colon-separated hex.
	want := "2C:F2:4D:BA:5F:B0:A3:0E:26:E8:3B:2A:C5:B9:E2:9E:1B:16:1E:5C:1F:A7:42:5E:73:04:33:62:93:8B:98:24"
	if fp != want {
		t.Errorf("fingerprint = %q, want %q", fp, want)
	}
}

// TestCheckCertKeyPermissions_WorldReadable warns (not errors) when
// the operator-supplied key file is loose. The serve path logs the
// warning and keeps booting, so we just need the warn string to be
// non-empty in the loose case and empty in the locked case.
func TestCheckCertKeyPermissions_WorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(path, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}
	_, warn, err := CheckCertKeyPermissions(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if warn == "" {
		t.Error("expected warning for 0644 key file, got none")
	}
}

func TestCheckCertKeyPermissions_Locked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(path, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	_, warn, err := CheckCertKeyPermissions(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if warn != "" {
		t.Errorf("expected no warning for 0600 key file, got %q", warn)
	}
}

func TestCheckCertKeyPermissions_Missing(t *testing.T) {
	_, _, err := CheckCertKeyPermissions("/definitely/not/a/real/path/key.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
