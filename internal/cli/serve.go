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

	// Token auth: verify against the argon2id hash file on disk. If the
	// file is missing, bootstrap it by generating a fresh token, printing
	// it ONCE to stderr, and persisting only the hash. The plaintext
	// token is never written anywhere else and never passes through slog
	// (gm-e5.2 DoD).
	if cfg.EffectiveAuthMode() == "token" && cfg.AuthToken == "" {
		if cfg.AuthTokenHashPath == "" {
			p, err := auth.DefaultTokenPath()
			if err != nil {
				return fmt.Errorf("resolve token path: %w", err)
			}
			cfg.AuthTokenHashPath = p
		}
		if err := ensurePrimaryToken(cfg.AuthTokenHashPath); err != nil {
			return err
		}
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

// ensurePrimaryToken makes sure path contains a valid argon2id hash. When
// the file is absent it generates a fresh token, prints the plaintext once
// to stderr, and persists the hash with 0600 permissions. Stderr (not the
// slog pipeline) is used deliberately so the token does not enter any
// structured log stream.
func ensurePrimaryToken(path string) error {
	existing, err := auth.ReadHash(path)
	if err != nil {
		return fmt.Errorf("read token hash: %w", err)
	}
	if existing != "" {
		return nil
	}
	tok, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("generate auth token: %w", err)
	}
	hash, err := auth.HashToken(tok)
	if err != nil {
		return fmt.Errorf("hash auth token: %w", err)
	}
	if err := auth.WriteHash(path, hash); err != nil {
		return fmt.Errorf("write token hash: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "==============================================================")
	fmt.Fprintln(os.Stderr, "  Gemba generated a new auth token. This is the ONLY time it")
	fmt.Fprintln(os.Stderr, "  will be shown. Copy it now.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Token:  "+tok)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Header: Authorization: Bearer <token>")
	fmt.Fprintln(os.Stderr, "  Hash:   "+path)
	fmt.Fprintln(os.Stderr, "==============================================================")
	fmt.Fprintln(os.Stderr)
	return nil
}
