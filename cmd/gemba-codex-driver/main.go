package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gemba-codex-driver:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("gemba-codex-driver", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		promptFile = fs.String("prompt-file", getenv("GEMBA_CODEX_PROMPT_FILE", ""), "prompt file written by gemba")
		beadID     = fs.String("bead", getenv("GEMBA_BEAD_ID", ""), "bead id to close and report")
		model      = fs.String("model", "", "Codex model")
		codexBin   = fs.String("codex-bin", "codex", "Codex CLI binary")
		sandbox    = fs.String("sandbox", "workspace-write", "Codex sandbox mode")
		approval   = fs.String("ask-for-approval", "never", "Codex approval mode")
		lastMsg    = fs.String("output-last-message", "", "optional Codex last-message output path")
		timeout    = fs.Duration("timeout", 45*time.Minute, "maximum Codex runtime")
		closeBead  = fs.Bool("close-bead", true, "close the bead after Codex exits successfully")
	)
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if strings.TrimSpace(*promptFile) == "" {
		return errors.New("GEMBA_CODEX_PROMPT_FILE or --prompt-file is required")
	}
	if strings.TrimSpace(*beadID) == "" {
		return errors.New("GEMBA_BEAD_ID or --bead is required")
	}
	prompt, err := os.ReadFile(*promptFile)
	if err != nil {
		return fmt.Errorf("read prompt file: %w", err)
	}

	_ = reportState(ctx, "working", *beadID, "codex exec started")

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	args := []string{
		"exec",
		"--json",
		"--sandbox", *sandbox,
		"--ask-for-approval", *approval,
		"--skip-git-repo-check",
		"--ephemeral",
		"-",
	}
	if *model != "" {
		args = append([]string{"exec", "--json", "--model", *model}, args[2:]...)
	}
	if *lastMsg != "" {
		args = append(args[:len(args)-1], "--output-last-message", *lastMsg, "-")
	}
	cmd := exec.CommandContext(runCtx, *codexBin, args...)
	cmd.Stdin = strings.NewReader(string(prompt))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		_ = reportState(ctx, "stalled", *beadID, fmt.Sprintf("codex exec failed: %v", err))
		return fmt.Errorf("codex exec: %w", err)
	}
	if *closeBead {
		if err := closeIssue(ctx, *beadID); err != nil {
			_ = reportState(ctx, "stalled", *beadID, fmt.Sprintf("bd close failed: %v", err))
			return err
		}
	}
	_ = commitDirtyWorktree(ctx, *beadID)
	if err := reportState(ctx, "bead-done", *beadID, "codex exec completed"); err != nil {
		return err
	}
	return nil
}

func reportState(ctx context.Context, state, beadID, note string) error {
	args := []string{state, "--bead", beadID}
	if note != "" {
		args = append(args, "--note", note)
	}
	cmd := exec.CommandContext(ctx, siblingOrPath("gemba-state"), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func closeIssue(ctx context.Context, beadID string) error {
	cmd := exec.CommandContext(ctx, "bd", "close", beadID, "--force", "--reason", "Completed by Codex via gemba-codex-driver")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bd close %s: %w", beadID, err)
	}
	return nil
}

func commitDirtyWorktree(ctx context.Context, beadID string) error {
	if err := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return nil
	}
	status := exec.CommandContext(ctx, "git", "status", "--porcelain")
	out, err := status.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil
	}
	if err := exec.CommandContext(ctx, "git", "add", "-A").Run(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx,
		"git",
		"-c", "user.name=Gemba Codex",
		"-c", "user.email=gemba-codex@example.invalid",
		"commit", "-m", "Complete "+beadID,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func siblingOrPath(name string) string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
