// egress.go — `gemba egress template …` CLI surface (gm-o9t8.4.3).
//
// Subcommands:
//
//	gemba egress template list                     — print the available templates
//	gemba egress template apply --workspace <wsid> --name <template>
//	                                                — seed (idempotent) the workspace's
//	                                                  egress rules from a template.
//
// `list` is pure-local (it reads the package-level egress.BuiltinTemplates
// bundle) and prints either a fixed-column table or JSON (--json).
//
// `apply` talks to a gemba server. It:
//
//   1. resolves the template by name (errors on unknown);
//   2. fetches the workspace's current rules via
//      GET  /api/v1/workspaces/{wsid}/egress-rules;
//   3. for every rule the template would seed that is not already
//      present (matched on ID), POSTs it to
//      POST /api/v1/workspaces/{wsid}/egress-rules.
//
// Re-applying the same template is a no-op — every template-derived
// rule carries a stable ID ("tpl-<name>-<host-pattern>") so the
// presence check skips it on the second pass.
//
// The "wide-open" template emits a slog.Warn on apply so the operator's
// logs record an explicit acknowledgement that the workspace is now
// effectively allow-all.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/egress"
)

// templateRuleID is the deterministic ID a template-seeded rule
// carries. It mirrors the server-side defaultEgressTemplateProvider so
// the CLI can presence-check without round-tripping the bundle.
func templateRuleID(tplName, host string) string {
	r := strings.ToLower(host)
	r = strings.ReplaceAll(r, ".", "-")
	r = strings.ReplaceAll(r, "*", "x")
	return "tpl-" + tplName + "-" + r
}

func newEgressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "egress",
		Short: "Manage workspace egress policy",
		Long: `Subcommands manage per-workspace egress rules.

  template list   — print the available egress templates
  template apply  — seed a workspace's egress allowlist from a named
                    template (idempotent)
`,
	}
	cmd.AddCommand(newEgressTemplateCmd())
	return cmd
}

func newEgressTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage egress policy templates",
	}
	cmd.AddCommand(newEgressTemplateListCmd())
	cmd.AddCommand(newEgressTemplateApplyCmd())
	return cmd
}

func newEgressTemplateListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available egress templates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			provider := egress.NewBuiltinTemplates()
			names := egress.TemplateNames()
			if asJSON {
				rows := make([]map[string]any, 0, len(names))
				for _, n := range names {
					tpl := provider.Lookup(n)
					rows = append(rows, map[string]any{
						"name":            n,
						"network_default": string(tpl.NetworkDefault),
						"hosts":           tpl.AllowedFQDNs,
					})
				}
				return json.NewEncoder(out).Encode(map[string]any{"templates": rows})
			}
			fmt.Fprintf(out, "%-14s  %-7s  %s\n", "NAME", "DEFAULT", "HOSTS")
			for _, n := range names {
				tpl := provider.Lookup(n)
				hosts := strings.Join(tpl.AllowedFQDNs, ",")
				if hosts == "" {
					hosts = "(none)"
				}
				fmt.Fprintf(out, "%-14s  %-7s  %s\n", n, tpl.NetworkDefault, hosts)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

// egressAPIClient is the narrow seam used by `template apply` so tests
// can substitute an httptest-backed transport without dragging the
// credstore in. Default implementation uses net/http directly against
// the resolved server URL.
type egressAPIClient interface {
	ListRules(ctx context.Context, wsid string) ([]egress.Rule, error)
	CreateRule(ctx context.Context, wsid string, rule egress.Rule) error
}

// egressAPIClientFactory builds an egressAPIClient bound to server.
// Tests swap via withEgressAPIClientFactory.
type egressAPIClientFactory func(server string) (egressAPIClient, error)

var egressAPIClientFactoryFn egressAPIClientFactory = func(server string) (egressAPIClient, error) {
	// Honour the same credstore-backed resolution as `gemba bead`. The
	// runClientFactory already handles WithToken + credstore lookup;
	// we wrap its *Client into the narrower http surface this command
	// needs.
	rc, err := runClientFactoryFn(server)
	if err != nil {
		return nil, err
	}
	return &httpEgressClient{server: strings.TrimRight(server, "/"), doer: rc}, nil
}

// withEgressAPIClientFactory swaps the factory; returns a restore func.
func withEgressAPIClientFactory(f egressAPIClientFactory) func() {
	prev := egressAPIClientFactoryFn
	egressAPIClientFactoryFn = f
	return func() { egressAPIClientFactoryFn = prev }
}

// httpDoer narrows the surface httpEgressClient consumes. The wrapper
// *client.Client satisfies this through its Do method (it stamps Auth
// + X-GEMBA-Confirm on the way through).
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type httpEgressClient struct {
	server string
	doer   httpDoer
}

func (c *httpEgressClient) ListRules(ctx context.Context, wsid string) ([]egress.Rule, error) {
	url := c.server + "/api/v1/workspaces/" + wsid + "/egress-rules"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("egress list: %s: %s", resp.Status, string(body))
	}
	var env struct {
		Rules []egress.Rule `json:"rules"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("egress list: decode: %w", err)
	}
	return env.Rules, nil
}

func (c *httpEgressClient) CreateRule(ctx context.Context, wsid string, rule egress.Rule) error {
	url := c.server + "/api/v1/workspaces/" + wsid + "/egress-rules"
	body, err := json.Marshal(map[string]any{
		"id":           rule.ID,
		"action":       string(rule.Action),
		"proto":        string(rule.Proto),
		"host_pattern": rule.HostPattern,
		"port_start":   rule.PortStart,
		"port_end":     rule.PortEnd,
		"priority":     rule.Priority,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doer.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("egress create: %s: %s", resp.Status, string(b))
	}
	return nil
}

func newEgressTemplateApplyCmd() *cobra.Command {
	var (
		workspace string
		name      string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Seed a workspace's egress allowlist from a template",
		Long: `Apply a named egress template to a workspace. The template's
host allowlist is materialized into per-workspace egress rules and
written to the server. Re-applying the same template is a no-op —
rules with the template's stable IDs are skipped.

The "wide-open" template emits a warning because it removes the
per-host allowlist entirely.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if workspace == "" {
				return errors.New("--workspace is required")
			}
			if name == "" {
				return errors.New("--name is required")
			}
			// Reject unknown names explicitly — the egress package
			// silently falls back to "default" which would mask a typo
			// from the operator.
			known := false
			for _, n := range egress.TemplateNames() {
				if strings.EqualFold(n, name) {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("unknown template %q (run `gemba egress template list`)", name)
			}

			provider := egress.NewBuiltinTemplates()
			tpl := provider.Lookup(name)

			if tpl.NetworkDefault == egress.NetworkAllow {
				slog.Warn("egress template applies a default-allow network stance",
					"template", tpl.Name,
					"workspace", workspace,
					"hint", "this workspace will effectively bypass egress filtering")
			}

			server, err := resolveServerFlag(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			api, err := egressAPIClientFactoryFn(server)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			existing, err := api.ListRules(ctx, workspace)
			if err != nil {
				return err
			}
			present := make(map[string]struct{}, len(existing))
			for _, r := range existing {
				present[r.ID] = struct{}{}
			}

			out := cmd.OutOrStdout()
			now := time.Now().UTC()
			created, skipped := 0, 0
			for i, host := range tpl.AllowedFQDNs {
				host = strings.TrimSpace(host)
				if host == "" {
					continue
				}
				id := templateRuleID(tpl.Name, host)
				if _, ok := present[id]; ok {
					skipped++
					continue
				}
				rule := egress.Rule{
					ID:          id,
					WorkspaceID: workspace,
					Action:      egress.ActionAllow,
					Proto:       egress.ProtoTCP,
					HostPattern: host,
					PortStart:   443,
					PortEnd:     443,
					Priority:    1000 - i,
					CreatedAt:   now,
					CreatedBy:   "egress-template:" + tpl.Name,
				}
				if err := api.CreateRule(ctx, workspace, rule); err != nil {
					return fmt.Errorf("apply rule %s: %w", id, err)
				}
				created++
				fmt.Fprintf(out, "created %s -> %s\n", id, host)
			}
			fmt.Fprintf(out, "template %q applied to %s: %d created, %d already-present\n",
				tpl.Name, workspace, created, skipped)
			return nil
		},
	}
	addServerFlags(cmd)
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id to seed (required)")
	cmd.Flags().StringVar(&name, "name", "", "template name (required; see `egress template list`)")
	_ = cmd.MarkFlagRequired("workspace")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
