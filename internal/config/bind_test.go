package config

import (
	"strings"
	"testing"
)

func TestValidateBindPolicy(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServeConfig
		wantErr bool
		errHas  string
	}{
		{
			name: "default loopback no auth is ok",
			cfg:  ServeConfig{Listen: "127.0.0.1"},
		},
		{
			name: "ipv6 loopback no auth is ok",
			cfg:  ServeConfig{Listen: "::1"},
		},
		{
			name: "empty listen (default) is ok",
			cfg:  ServeConfig{},
		},
		{
			name: "loopback with token auth is ok (redundant but allowed)",
			cfg:  ServeConfig{Listen: "127.0.0.1", AuthMode: "token"},
		},
		{
			name:    "0.0.0.0 without auth is rejected",
			cfg:     ServeConfig{Listen: "0.0.0.0"},
			wantErr: true,
			errHas:  "non-loopback",
		},
		{
			name:    "0.0.0.0 with auth=none is rejected",
			cfg:     ServeConfig{Listen: "0.0.0.0", AuthMode: "none"},
			wantErr: true,
			errHas:  "non-loopback",
		},
		{
			name: "0.0.0.0 with token auth is ok",
			cfg:  ServeConfig{Listen: "0.0.0.0", AuthMode: "token"},
		},
		{
			name: "0.0.0.0 with oidc auth is ok",
			cfg:  ServeConfig{Listen: "0.0.0.0", AuthMode: "oidc"},
		},
		{
			name:    "ipv6 unspecified without auth is rejected",
			cfg:     ServeConfig{Listen: "::"},
			wantErr: true,
			errHas:  "non-loopback",
		},
		{
			name: "localhost hostname no auth is ok",
			cfg:  ServeConfig{Listen: "localhost"},
		},
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
