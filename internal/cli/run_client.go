// run_client.go: shared typed-client factory + flag plumbing for the
// `gemba run`, `gemba logs`, and `gemba agent dispatch` verbs
// (gm-o9t8.1.11).
//
// Mirrors the seam in bead_crud.go (crudFactory): tests swap the
// factory to bind a typed client to an httptest.Server without
// touching the on-disk credstore.
//
// Flag normalization (gm-o9t8.1.13): `--server` is the canonical flag.
// `--base-url` remains as a hidden deprecated alias; using it emits a
// one-line stderr warning the first time it's read on a given command.

package cli

import (
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	gembaclient "github.com/GembaCore/gemba-core/internal/client"
	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
)

// runClientFactory builds a typed client bound to server. Tests
// override via withRunClientFactory.
type runClientFactory func(server string) (*gembaclient.Client, error)

// defaultRunClientFactory consults the credstore for server's token,
// matching the production behavior of `gemba bead …` and `gemba whoami`.
var runClientFactoryFn runClientFactory = func(server string) (*gembaclient.Client, error) {
	return gembaclient.New(server)
}

// withRunClientFactory swaps the factory for the duration of the
// returned restore func. Test-only helper.
func withRunClientFactory(f runClientFactory) func() {
	prev := runClientFactoryFn
	runClientFactoryFn = f
	return func() { runClientFactoryFn = prev }
}

// addServerFlags installs the canonical --server flag plus the hidden
// --base-url alias on cmd. Call from each command's constructor.
func addServerFlags(cmd *cobra.Command) {
	cmd.Flags().String("server", "", "gemba server URL (overrides GEMBA_SERVER and config.toml)")
	cmd.Flags().String("base-url", "", "deprecated alias for --server")
	_ = cmd.Flags().MarkHidden("base-url")
}

// resolveServerFlag reads --server then --base-url, emitting a one-time
// deprecation warning to stderr when --base-url supplies the value.
// Falls through to the standard serverconfig precedence
// (env → config.toml → DefaultURL).
func resolveServerFlag(cmd *cobra.Command, stderr io.Writer) (string, error) {
	server, _ := cmd.Flags().GetString("server")
	if server == "" {
		baseURL, _ := cmd.Flags().GetString("base-url")
		if baseURL != "" {
			fmt.Fprintln(stderr, "warning: --base-url is deprecated; use --server instead")
			server = baseURL
		}
	}
	return serverconfig.Resolve(server)
}

// httpClientFactoryForTests is a convenience for run_test / logs_test /
// agent_dispatch_test helpers: builds a typed client bound to srvURL
// with a fixed test token and the default HTTP transport, skipping the
// credstore entirely.
func httpClientFactoryForTests(srvURL string) runClientFactory {
	return func(server string) (*gembaclient.Client, error) {
		return gembaclient.New(srvURL,
			gembaclient.WithToken("TESTPAT"),
			gembaclient.WithHTTPClient(http.DefaultClient),
		)
	}
}
