package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ghAuthTimeout bounds the gh auth probe so a slow or wedged gh never stalls
// the TUI; the check is a local token lookup and returns near-instantly.
const ghAuthTimeout = 4 * time.Second

// GHAvailable reports whether the GitHub CLI is installed and on PATH.
func GHAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// GHAuthenticated reports whether gh holds working credentials for host. This
// is the only clone path that succeeds for a private github.com repo on a
// machine with no ssh key. It never prompts and gives up quickly.
func GHAuthenticated(ctx context.Context, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, ghAuthTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "status", "--hostname", host)
	cmd.Env = nonInteractiveGitEnv()
	return cmd.Run() == nil
}

// ghClone clones ownerRepo (e.g. "owner/repo") into dest via the GitHub CLI,
// which reuses gh's stored token without any local ssh key. It never prompts.
func ghClone(ctx context.Context, ownerRepo, dest string) error {
	cmd := exec.CommandContext(ctx, "gh", "repo", "clone", ownerRepo, dest)
	cmd.Env = nonInteractiveGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return ghError(err, stdout.String(), stderr.String())
	}
	return nil
}

// ghError formats a gh failure with any credentials redacted from its output.
func ghError(err error, stdout, stderr string) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = strings.TrimSpace(stdout)
	}
	detail = RedactURL(detail)
	if detail == "" {
		return err
	}
	return fmt.Errorf("%s: %w", detail, err)
}
