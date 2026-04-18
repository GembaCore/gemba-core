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

func TestEffectiveAuthMode(t *testing.T) {
	if got := (ServeConfig{}).EffectiveAuthMode(); got != "none" {
		t.Fatalf("default should be none, got %q", got)
	}
	if got := (ServeConfig{AuthMode: "token"}).EffectiveAuthMode(); got != "token" {
		t.Fatalf("want token, got %q", got)
	}
}
