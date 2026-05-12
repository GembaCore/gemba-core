package egress

import "testing"

func TestMatchHost_Matrix(t *testing.T) {
	cases := []struct {
		pattern string
		host    string
		want    bool
	}{
		// exact literal
		{"api.anthropic.com", "api.anthropic.com", true},
		{"api.anthropic.com", "API.Anthropic.COM", true}, // case-insensitive
		{"api.anthropic.com", "anthropic.com", false},
		{"api.anthropic.com", "foo.api.anthropic.com", false},

		// single-label wildcard
		{"*.anthropic.com", "api.anthropic.com", true},
		{"*.anthropic.com", "foo.bar.anthropic.com", false},
		{"*.anthropic.com", "anthropic.com", false},

		// multi-label wildcard
		{"**.anthropic.com", "api.anthropic.com", true},
		{"**.anthropic.com", "foo.bar.anthropic.com", true},
		{"**.anthropic.com", "anthropic.com", false},
		{"**.anthropic.com", "evil.com", false},

		// bare wildcards
		{"*", "localhost", true},
		{"*", "a.b", false},
		{"**", "anything.you.want", true},
		{"**", "", false},

		// wildcards in non-leading positions
		{"api.*.com", "api.openai.com", true},
		{"api.*.com", "api.foo.bar.com", false},
		{"api.**.com", "api.foo.bar.com", true},

		// empty inputs
		{"", "host", false},
		{"pattern", "", false},
	}
	for _, tc := range cases {
		got := MatchHost(tc.pattern, tc.host)
		if got != tc.want {
			t.Errorf("MatchHost(%q, %q) = %v, want %v", tc.pattern, tc.host, got, tc.want)
		}
	}
}

func TestMatchRule_ProtoAndPort(t *testing.T) {
	r := Rule{
		Action:      ActionAllow,
		Proto:       ProtoTCP,
		HostPattern: "api.openai.com",
		PortStart:   443,
		PortEnd:     443,
	}
	if !MatchRule(r, ProtoTCP, "api.openai.com", 443) {
		t.Fatal("expected exact match")
	}
	if MatchRule(r, ProtoUDP, "api.openai.com", 443) {
		t.Fatal("UDP must not match a TCP rule")
	}
	if MatchRule(r, ProtoTCP, "api.openai.com", 80) {
		t.Fatal("port mismatch must not match")
	}
	any := Rule{Action: ActionDeny, Proto: ProtoAny, HostPattern: "**"}
	if !MatchRule(any, ProtoUDP, "anything.tld", 53) {
		t.Fatal("ProtoAny + ** + zero ports must match everything")
	}
}
