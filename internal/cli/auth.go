package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/auth"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage gemba authentication credentials",
	}
	cmd.AddCommand(newTokenCmd())
	return cmd
}

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage the primary auth token",
	}
	cmd.AddCommand(newTokenRotateCmd())
	return cmd
}

func newTokenRotateCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate the primary auth token and print the new value once",
		Long: `Generate a fresh 256-bit token, persist its argon2id hash, and print
the new plaintext token ONCE to stdout.

A running 'gemba serve' picks up the new hash on the next request — no
restart required — because the verifier stats the hash file on every
auth attempt and reloads when the file's mtime changes.

The plaintext token is printed to stdout so it can be piped into a
keystore. Nothing else writes the token anywhere.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := path
			if p == "" {
				resolved, err := auth.DefaultTokenPath()
				if err != nil {
					return err
				}
				p = resolved
			}
			return runTokenRotate(cmd.OutOrStdout(), cmd.ErrOrStderr(), p)
		},
	}
	cmd.Flags().StringVar(&path, "path", "",
		"override the hash file path (default: ~/.gemba/tokens/primary)")
	return cmd
}

func runTokenRotate(out, stderr io.Writer, path string) error {
	tok, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	hash, err := auth.HashToken(tok)
	if err != nil {
		return fmt.Errorf("hash token: %w", err)
	}
	if err := auth.WriteHash(path, hash); err != nil {
		return fmt.Errorf("persist token hash: %w", err)
	}
	fmt.Fprintln(out, tok)
	fmt.Fprintf(stderr, "rotated token hash at %s\n", path)
	return nil
}
