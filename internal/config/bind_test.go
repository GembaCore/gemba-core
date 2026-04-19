package config

import (
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

func TestEffectiveAuthMode(t *testing.T) {
	if got := (ServeConfig{}).EffectiveAuthMode(); got != "none" {
		t.Fatalf("default should be none, got %q", got)
	}
	if got := (ServeConfig{AuthMode: "token"}).EffectiveAuthMode(); got != "token" {
		t.Fatalf("want token, got %q", got)
	}
}
