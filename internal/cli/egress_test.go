package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// stubEgressAPIClient is the in-memory egressAPIClient the apply tests
// inject so they don't need an httptest server.
type stubEgressAPIClient struct {
	mu      sync.Mutex
	rules   map[string]egress.Rule
	created []egress.Rule
	listErr error
}

func newStubEgressAPIClient(existing ...egress.Rule) *stubEgressAPIClient {
	s := &stubEgressAPIClient{rules: make(map[string]egress.Rule, len(existing))}
	for _, r := range existing {
		s.rules[r.ID] = r
	}
	return s
}

func (s *stubEgressAPIClient) ListRules(_ context.Context, _ string) ([]egress.Rule, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]egress.Rule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r)
	}
	return out, nil
}

func (s *stubEgressAPIClient) CreateRule(_ context.Context, _ string, rule egress.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.ID] = rule
	s.created = append(s.created, rule)
	return nil
}

func TestEgressTemplateList_Table(t *testing.T) {
	cmd := newEgressTemplateListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NAME", "github-only", "pypi+npm", "wide-open"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table missing %q\n---\n%s", want, out.String())
		}
	}
}

func TestEgressTemplateList_JSON(t *testing.T) {
	cmd := newEgressTemplateListCmd()
	cmd.SetArgs([]string{"--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var env struct {
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v: %s", err, out.String())
	}
	if len(env.Templates) < 3 {
		t.Fatalf("want at least 3 templates, got %d", len(env.Templates))
	}
}

func TestEgressTemplateApply_SeedsRules(t *testing.T) {
	stub := newStubEgressAPIClient()
	restore := withEgressAPIClientFactory(func(_ string) (egressAPIClient, error) {
		return stub, nil
	})
	defer restore()

	cmd := newEgressTemplateApplyCmd()
	cmd.SetArgs([]string{"--workspace", "ws-test", "--name", "github-only", "--server", "http://127.0.0.1:0"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply: %v\n%s", err, out.String())
	}
	if len(stub.created) == 0 {
		t.Fatalf("no rules created\n%s", out.String())
	}
	want := templateRuleID("github-only", "github.com")
	if _, ok := stub.rules[want]; !ok {
		t.Fatalf("expected rule %s in stub, got %+v", want, stub.rules)
	}
}

func TestEgressTemplateApply_Idempotent(t *testing.T) {
	provider := egress.NewBuiltinTemplates()
	tpl := provider.Lookup("github-only")
	pre := make([]egress.Rule, 0, len(tpl.AllowedFQDNs))
	for _, host := range tpl.AllowedFQDNs {
		pre = append(pre, egress.Rule{
			ID:          templateRuleID(tpl.Name, host),
			WorkspaceID: "ws-test",
			HostPattern: host,
		})
	}
	stub := newStubEgressAPIClient(pre...)
	restore := withEgressAPIClientFactory(func(_ string) (egressAPIClient, error) {
		return stub, nil
	})
	defer restore()

	cmd := newEgressTemplateApplyCmd()
	cmd.SetArgs([]string{"--workspace", "ws-test", "--name", "github-only", "--server", "http://127.0.0.1:0"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply re-run: %v\n%s", err, out.String())
	}
	if len(stub.created) != 0 {
		t.Fatalf("expected idempotent no-op, got %d creates", len(stub.created))
	}
	if !strings.Contains(out.String(), "already-present") {
		t.Fatalf("expected already-present summary, got:\n%s", out.String())
	}
}

func TestEgressTemplateApply_UnknownName(t *testing.T) {
	cmd := newEgressTemplateApplyCmd()
	cmd.SetArgs([]string{"--workspace", "ws-test", "--name", "no-such-template", "--server", "http://127.0.0.1:0"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("got %v", err)
	}
}
