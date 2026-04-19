package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gemba "github.com/MikeBengtson/gemba"
	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/server"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var cfg config.ServeConfig

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Gemba HTTP server",
		Long: `Runs the Gemba HTTP server.

By default binds to 127.0.0.1:7666 with no authentication. To expose on the
network, pass --listen with a non-loopback address AND --auth to enable
authentication. Binding a non-loopback interface without --auth is an error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.NormalizeListen(cmd.Flags().Changed("port")); err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg)
		},
	}

	cmd.Flags().StringVar(&cfg.Listen, "listen", "127.0.0.1",
		"interface to bind to, or host:port (e.g. 127.0.0.1:7666, [::1]:7666); "+
			"use 0.0.0.0 for all interfaces (requires --auth)")
	cmd.Flags().IntVar(&cfg.Port, "port", 7666, "TCP port to listen on")
	cmd.Flags().BoolVar(&cfg.Open, "open", false,
		"open the UI in a browser after starting")

	cmd.Flags().StringVar(&cfg.AuthMode, "auth", "",
		"authentication mode: none (default), token, oidc")
	cmd.Flags().StringVar(&cfg.TLSCert, "tls-cert", "",
		"path to TLS certificate (use --tls-self-signed to auto-generate)")
	cmd.Flags().StringVar(&cfg.TLSKey, "tls-key", "",
		"path to TLS key file")
	cmd.Flags().BoolVar(&cfg.TLSSelfSigned, "tls-self-signed", false,
		"generate a self-signed TLS certificate on first run")

	cmd.Flags().StringVar(&cfg.City, "city", "",
		"path to Gas City workspace (default: auto-detect city.toml or ~/my-city)")
	cmd.Flags().StringVar(&cfg.Town, "town", "",
		"path to Gas Town HQ (legacy; prefer --city for Gas City workspaces)")

	cmd.Flags().StringVar(&cfg.ConfigPath, "config", "",
		"path to gemba.toml (default: probe cwd, then ~/.config/gemba/)")

	// Flag name copied verbatim from Claude Code. Do not rename or soften.
	cmd.Flags().BoolVar(&cfg.DangerouslySkipPermissions,
		"dangerously-skip-permissions", false,
		"disable mutation confirmation prompts for this server session "+
			"(name copied from Claude Code; intentional)")

	return cmd
}

func runServe(ctx context.Context, cfg config.ServeConfig) error {
	if err := cfg.ValidateBindPolicy(); err != nil {
		return err
	}
	if cfg.DangerouslySkipPermissions {
		slog.Warn("DANGEROUSLY-SKIP-PERMISSIONS IS ACTIVE",
			"note", "mutations will not require confirmation for this session")
	}

	// Generate a token when the operator opts into token auth but hasn't
	// supplied one. Printed once at startup so it can be copied; never
	// re-logged on subsequent requests (gm-99g / gm-b3).
	if cfg.EffectiveAuthMode() == "token" && cfg.AuthToken == "" {
		tok, err := auth.NewToken()
		if err != nil {
			return fmt.Errorf("generate auth token: %w", err)
		}
		cfg.AuthToken = tok
		slog.Info("generated token auth credential",
			"token", tok,
			"usage", "pass header: Authorization: Bearer <token>")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Listen, cfg.Port)
	spa := gemba.SPA()
	if spa == nil {
		slog.Warn("SPA not embedded in this binary",
			"hint", "run `make build` to embed web/dist; "+
				"non-API routes will return a 503 build hint")
	}
	handler := server.NewRouter(cfg, spa)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		scheme := "http"
		if cfg.TLSCert != "" || cfg.TLSSelfSigned {
			scheme = "https"
		}
		slog.Info("gemba listening",
			"url", fmt.Sprintf("%s://%s", scheme, addr),
			"auth", cfg.EffectiveAuthMode())

		var err error
		if scheme == "https" {
			err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(), 10*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdownCtx)
}
