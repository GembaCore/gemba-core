package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBindPolicy(t *testing.T) {
	// Bind matrix × auth matrix. Loopback bypasses the gate for every auth
	// mode; non-loopback requires a non-"none" auth mode.
	loopbackHosts := []string{"127.0.0.1", "::1", "localhost", ""}
	nonLoopbackHosts := []string{
		"0.0.0.0",     // ipv4 unspecified
		"::",          // ipv6 unspecified
		"192.168.1.5", // typical LAN
		"10.0.0.1",    // private RFC1918
		"172.16.0.1",  // private RFC1918
	}
	authModes := []string{"", "none", "token", "oidc"}

	cases := []struct {
		name    string
		cfg     ServeConfig
		wantErr bool
		errHas  string
	}{}

	for _, host := range loopbackHosts {
		for _, mode := range authModes {
			cases = append(cases, struct {
				name    string
				cfg     ServeConfig
				wantErr bool
				errHas  string
			}{
				name: "loopback " + showHost(host) + " auth=" + showAuth(mode) + " ok",
				cfg:  ServeConfig{Listen: host, AuthMode: mode},
			})
		}
	}
	for _, host := range nonLoopbackHosts {
		for _, mode := range authModes {
			gate := mode == "" || mode == "none"
			c := struct {
				name    string
				cfg     ServeConfig
				wantErr bool
				errHas  string
			}{
				name: "non-loopback " + host + " auth=" + showAuth(mode),
				cfg:  ServeConfig{Listen: host, Port: 7666, AuthMode: mode},
			}
			if gate {
				c.wantErr = true
				c.errHas = "non-loopback bind requires --auth"
			}
			cases = append(cases, c)
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateBindPolicy()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errHas != "" && !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestValidateBindPolicy_ErrorMentionsListen pins the error format required
// by gm-e5.1: the operator's --listen value (with port) must appear verbatim
// so they can immediately see which intent was rejected.
func TestValidateBindPolicy_ErrorMentionsListen(t *testing.T) {
	cases := []struct {
		listen string
		port   int
		want   string
	}{
		{"0.0.0.0", 7666, "--listen 0.0.0.0:7666"},
		{"192.168.1.5", 9000, "--listen 192.168.1.5:9000"},
		{"::", 7666, "--listen [::]:7666"},
	}
	for _, tc := range cases {
		t.Run(tc.listen, func(t *testing.T) {
			err := (ServeConfig{Listen: tc.listen, Port: tc.port}).ValidateBindPolicy()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

func showHost(h string) string {
	if h == "" {
		return "(default)"
	}
	return h
}

func showAuth(m string) string {
	if m == "" {
		return "(default)"
	}
	return m
}

func TestNormalizeListen(t *testing.T) {
	cases := []struct {
		name        string
		in          ServeConfig
		portFlagSet bool
		wantListen  string
		wantPort    int
		wantErr     bool
		errHas      string
	}{
		{
			name:       "interface only is unchanged",
			in:         ServeConfig{Listen: "127.0.0.1", Port: 7666},
			wantListen: "127.0.0.1",
			wantPort:   7666,
		},
		{
			name:       "ipv4 host:port is split",
			in:         ServeConfig{Listen: "127.0.0.1:8080", Port: 7666},
			wantListen: "127.0.0.1",
			wantPort:   8080,
		},
		{
			name:       "ipv6 bracketed host:port is split",
			in:         ServeConfig{Listen: "[::1]:8080", Port: 7666},
			wantListen: "::1",
			wantPort:   8080,
		},
		{
			name:       "hostname:port is split",
			in:         ServeConfig{Listen: "localhost:9000", Port: 7666},
			wantListen: "localhost",
			wantPort:   9000,
		},
		{
			name:       "0.0.0.0 with port is split",
			in:         ServeConfig{Listen: "0.0.0.0:9000", Port: 7666},
			wantListen: "0.0.0.0",
			wantPort:   9000,
		},
		{
			name:       "bare ipv6 is left alone",
			in:         ServeConfig{Listen: "::1", Port: 7666},
			wantListen: "::1",
			wantPort:   7666,
		},
		{
			name:       "empty listen is left alone",
			in:         ServeConfig{Port: 7666},
			wantListen: "",
			wantPort:   7666,
		},
		{
			name:        "host:port + explicit --port is rejected",
			in:          ServeConfig{Listen: "127.0.0.1:8080", Port: 9000},
			portFlagSet: true,
			wantErr:     true,
			errHas:      "port specified twice",
		},
		{
			name:    "non-numeric port is rejected",
			in:      ServeConfig{Listen: "127.0.0.1:abc"},
			wantErr: true,
			errHas:  "invalid port",
		},
		{
			name:    "out-of-range port is rejected",
			in:      ServeConfig{Listen: "127.0.0.1:99999"},
			wantErr: true,
			errHas:  "invalid port",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.in
			err := cfg.NormalizeListen(tc.portFlagSet)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errHas != "" && !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Listen != tc.wantListen {
				t.Fatalf("Listen: got %q, want %q", cfg.Listen, tc.wantListen)
			}
			if cfg.Port != tc.wantPort {
				t.Fatalf("Port: got %d, want %d", cfg.Port, tc.wantPort)
			}
		})
	}
}

func TestResolveBeadsDir(t *testing.T) {
	// Build a realistic rig layout: tmp/rig/.beads/, a plain dir with no
	// .beads/, and a regular file. EvalSymlinks because macOS TempDir
	// resolves through /private/var and filepath.Abs does not collapse
	// that, which would defeat the exact-match assertion below.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	rig := filepath.Join(root, "rig")
	beads := filepath.Join(rig, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	plainDir := filepath.Join(root, "plain")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	regularFile := filepath.Join(root, "not-a-dir.txt")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		wantOut string
		wantErr string // substring; empty means "no error expected"
	}{
		{name: "empty passes through", in: "", wantOut: ""},
		{name: "rig root with .beads", in: rig, wantOut: rig},
		{name: "dir IS .beads", in: beads, wantOut: rig},
		{name: "missing path", in: filepath.Join(root, "nope"),
			wantErr: "does not exist"},
		{name: "regular file", in: regularFile,
			wantErr: "not a directory"},
		{name: "dir without .beads", in: plainDir,
			wantErr: "no .beads/ directory found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ServeConfig{ProjectDir: tc.in}.ResolveBeadsDir()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%q)", got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantOut {
				t.Fatalf("resolved dir: got %q, want %q", got, tc.wantOut)
			}
		})
	}
}

func TestValidateWorkPlaneFlags(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServeConfig
		wantErr string // substring; empty means "no error expected"
	}{
		{"neither set", ServeConfig{}, "no WorkPlane selected"},
		{"only project-dir", ServeConfig{ProjectDir: "/tmp/gm"}, ""},
		{"legacy beads-dir", ServeConfig{BeadsDir: "/tmp/gm"}, ""},
		{"only dolt-url", ServeConfig{DoltURL: "mysql://root@127.0.0.1:3307/gemba"}, ""},
		{"both set", ServeConfig{BeadsDir: "/tmp/gm", DoltURL: "mysql://root@127.0.0.1:3307/gemba"}, "mutually exclusive"},
		{"noop alone", ServeConfig{Noop: true}, ""},
		{"noop with project-dir", ServeConfig{Noop: true, ProjectDir: "/tmp/gm"}, "--noop is mutually exclusive"},
		{"noop with dolt-url", ServeConfig{Noop: true, DoltURL: "mysql://root@127.0.0.1:3307/gemba"}, "--noop is mutually exclusive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateWorkPlaneFlags()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error missing %q; got %q", tc.wantErr, err.Error())
			}
		})
	}
}

// TestValidateWorkPlaneFlags_ProjectDirWithEmbeddedDolt_Rejected pins
// gm-o9t8.1.15: --project-dir owns its own Dolt config in .beads/, so
// running the embedded-dolt supervisor alongside leaves the supervisor
// running but unused. The combination must be rejected unless the
// operator explicitly opted out via --embedded-dolt=false.
func TestValidateWorkPlaneFlags_ProjectDirWithEmbeddedDolt_Rejected(t *testing.T) {
	cfg := ServeConfig{
		ProjectDir:      "/tmp/gm",
		EmbeddedDolt:    true,
		EmbeddedDoltSet: true,
	}
	err := cfg.ValidateWorkPlaneFlags()
	if err == nil {
		t.Fatalf("want error for --project-dir + --embedded-dolt; got nil")
	}
	for _, want := range []string{"--project-dir", "--embedded-dolt", "mutually exclusive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got %q", want, err.Error())
		}
	}

	// Explicit opt-out (--embedded-dolt=false) must be accepted.
	cfgOptOut := ServeConfig{
		ProjectDir:      "/tmp/gm",
		EmbeddedDolt:    false,
		EmbeddedDoltSet: true,
	}
	if err := cfgOptOut.ValidateWorkPlaneFlags(); err != nil {
		t.Fatalf("--embedded-dolt=false should be accepted; got %v", err)
	}

	// Default (--embedded-dolt unset) must be accepted: applyEmbeddedDoltDefault
	// will turn it off when --project-dir is set; the validator only
	// rejects an explicit --embedded-dolt=true.
	cfgDefault := ServeConfig{ProjectDir: "/tmp/gm", EmbeddedDolt: true}
	if err := cfgDefault.ValidateWorkPlaneFlags(); err != nil {
		t.Fatalf("--embedded-dolt unset should be accepted; got %v", err)
	}
}

// TestValidateWorkPlaneFlags_NeitherSetMentionsBothFlags pins the
// shape of the "no WorkPlane selected" error: both flag names must
// appear verbatim so the operator can copy-paste the fix without
// digging through --help.
func TestValidateWorkPlaneFlags_NeitherSetMentionsBothFlags(t *testing.T) {
	err := (ServeConfig{}).ValidateWorkPlaneFlags()
	if err == nil {
		t.Fatalf("want error")
	}
	for _, want := range []string{"--project-dir", "--dolt-url"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q; got %q", want, err.Error())
		}
	}
}

// TestValidateTLSFlags pins the gm-e5.3 selector contract: --tls-cert
// and --tls-key are inseparable; either of them is mutually exclusive
// with --tls-self-signed; nothing set is fine (plain HTTP). The
// startup gate must reject conflicting input before the listener
// opens so the operator gets a clean error rather than a half-loaded
// TLS chain.
func TestValidateTLSFlags(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServeConfig
		wantErr string // substring; empty means "no error expected"
	}{
		{"neither set", ServeConfig{}, ""},
		{"self-signed only", ServeConfig{TLSSelfSigned: true}, ""},
		{"cert+key together", ServeConfig{TLSCert: "/c.pem", TLSKey: "/k.pem"}, ""},
		{"cert without key", ServeConfig{TLSCert: "/c.pem"}, "must be set together"},
		{"key without cert", ServeConfig{TLSKey: "/k.pem"}, "must be set together"},
		{"self-signed + cert/key",
			ServeConfig{TLSCert: "/c.pem", TLSKey: "/k.pem", TLSSelfSigned: true},
			"mutually exclusive"},
		{"self-signed + cert alone",
			ServeConfig{TLSCert: "/c.pem", TLSSelfSigned: true},
			// cert without key fires first; that's fine — both error
			// paths are operator-actionable
			"must be set together"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidateTLSFlags()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error missing %q; got %q", tc.wantErr, err.Error())
			}
		})
	}
}

// TestTLSEnabled covers the small predicate the serve goroutine uses
// to decide between ListenAndServe and ListenAndServeTLS.
func TestTLSEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServeConfig
		want bool
	}{
		{"empty", ServeConfig{}, false},
		{"self-signed", ServeConfig{TLSSelfSigned: true}, true},
		{"cert+key", ServeConfig{TLSCert: "/c", TLSKey: "/k"}, true},
		{"cert alone", ServeConfig{TLSCert: "/c"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.TLSEnabled(); got != tc.want {
				t.Errorf("TLSEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEffectiveAuthMode(t *testing.T) {
	if got := (ServeConfig{}).EffectiveAuthMode(); got != "none" {
		t.Fatalf("default should be none, got %q", got)
	}
	if got := (ServeConfig{AuthMode: "token"}).EffectiveAuthMode(); got != "token" {
		t.Fatalf("want token, got %q", got)
	}
}

func TestBeadsSource(t *testing.T) {
	cases := []struct {
		name       string
		cfg        ServeConfig
		wantKind   string
		wantLabel  string
		wantDetail string
	}{
		{
			name:       "project-dir",
			cfg:        ServeConfig{ProjectDir: "/Users/mike/gt/gemba"},
			wantKind:   "project-dir",
			wantLabel:  "gemba",
			wantDetail: "/Users/mike/gt/gemba",
		},
		{
			name:       "dolt-url with creds is redacted",
			cfg:        ServeConfig{DoltURL: "mysql://root:hunter2@127.0.0.1:3307/lume_spark_api"},
			wantKind:   "dolt-url",
			wantLabel:  "lume_spark_api",
			wantDetail: "mysql://127.0.0.1:3307/lume_spark_api",
		},
		{
			name:       "dolt-url with user only is redacted",
			cfg:        ServeConfig{DoltURL: "mysql://root@127.0.0.1:3307/gemba"},
			wantKind:   "dolt-url",
			wantLabel:  "gemba",
			wantDetail: "mysql://127.0.0.1:3307/gemba",
		},
		{
			name:       "dolt-url without creds is unchanged",
			cfg:        ServeConfig{DoltURL: "mysql://127.0.0.1:3307/sb"},
			wantKind:   "dolt-url",
			wantLabel:  "sb",
			wantDetail: "mysql://127.0.0.1:3307/sb",
		},
		{
			name:     "unconfigured",
			cfg:      ServeConfig{},
			wantKind: "unconfigured",
		},
		{
			name:       "noop",
			cfg:        ServeConfig{Noop: true},
			wantKind:   "noop",
			wantLabel:  "noop",
			wantDetail: "in-memory reference adaptor",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.BeadsSource()
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", got.Label, tc.wantLabel)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", got.Detail, tc.wantDetail)
			}
			// Defense in depth: the redacted Detail must never carry
			// the password literal. Cheap check, catches refactor
			// regressions even if Label/Detail shape changes.
			if strings.Contains(got.Detail, "hunter2") {
				t.Errorf("Detail leaks password: %q", got.Detail)
			}
		})
	}
}
