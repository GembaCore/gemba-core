// config.go — `gemba config` subcommands (gm-o9t8.1.1.3).
//
// Today the only verb is `gemba config show`, which prints the
// resolved server URL and the precedence layer it came from. The
// command exists so operators can diagnose "why is `gemba bead list`
// hitting the wrong server?" without instrumenting the binary.
//
// Layers, highest to lowest:
//
//  1. --server flag                          (Source: flag)
//  2. GEMBA_SERVER env var                   (Source: env)
//  3. $XDG_CONFIG_HOME/gemba/config.toml     (Source: file <path>)
//  4. compiled-in default 127.0.0.1:7666     (Source: default)

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect gemba CLI configuration",
		Long: `Subcommands inspect how the gemba CLI is resolving its
configuration. Today only 'show' is available, which prints the
resolved server URL and where it came from.`,
	}
	cmd.AddCommand(newConfigShowCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var (
		server string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved server URL and its source",
		Long: `Resolves the gemba server URL using the documented precedence
and prints both the URL and the layer it came from. This is the
canonical way to debug "why is gemba talking to the wrong server?".

Precedence (highest to lowest):

  1. --server flag                       (source: flag)
  2. GEMBA_SERVER environment variable   (source: env)
  3. server.url in config.toml at
     $XDG_CONFIG_HOME/gemba/config.toml
     (default ~/.config/gemba/config.toml) (source: file <path>)
  4. compiled-in default http://127.0.0.1:7666 (source: default)

The --server flag here lets you preview what a verb invoked with
--server <url> would resolve to.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			detail, err := serverconfig.ResolveDetail(server)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				body := map[string]string{
					"url":    detail.URL,
					"source": string(detail.Source),
				}
				if detail.File != "" {
					body["file"] = detail.File
				}
				return json.NewEncoder(out).Encode(body)
			}
			src := string(detail.Source)
			if detail.Source == serverconfig.SourceFile && detail.File != "" {
				src = fmt.Sprintf("file %s", detail.File)
			}
			fmt.Fprintf(out, "server: %s\nsource: %s\n", detail.URL, src)
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "preview the resolved URL as if this flag were passed to a verb")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable block")
	return cmd
}
