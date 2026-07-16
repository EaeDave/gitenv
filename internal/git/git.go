package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var credentialsInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

func Init(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create git repository directory: %w", err)
	}
	_, err := run(root, "init")
	if err != nil {
		return fmt.Errorf("initialize git repository: %w", err)
	}
	return nil
}

func Clone(remote, dest string) error {
	absolute, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolve clone destination: %w", err)
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create clone parent directory: %w", err)
	}
	_, err = run(parent, "clone", "--", remote, absolute)
	if err != nil {
		return fmt.Errorf("clone git repository: %w", err)
	}
	return nil
}

func Status(root string) (string, error) {
	output, err := run(root, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("read git status: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func Pull(root string) error {
	if _, err := run(root, "fetch"); err != nil {
		return fmt.Errorf("fetch git repository: %w", err)
	}
	if _, err := run(root, "merge", "--ff-only", "@{upstream}"); err != nil {
		return fmt.Errorf("fast-forward git repository: %w", err)
	}
	return nil
}

// Push publishes existing local commits without staging or committing working-tree changes.
func Push(root string) error {
	if hasUpstream(root) {
		if _, err := run(root, "push"); err != nil {
			return fmt.Errorf("push git repository: %w", err)
		}
		return nil
	}
	if _, err := run(root, "push", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push git repository: %w", err)
	}
	return nil
}

func CommitAndPush(root, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("commit message must not be empty")
	}
	upstream := hasUpstream(root)
	if upstream {
		if err := Pull(root); err != nil {
			return fmt.Errorf("update before commit: %w", err)
		}
	}
	if _, err := run(root, "add", "--all"); err != nil {
		return fmt.Errorf("stage git changes: %w", err)
	}

	_, _, err := command(root, "diff", "--cached", "--quiet")
	if err == nil {
		return errors.New("nothing to commit")
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		return fmt.Errorf("check staged git changes: %w", commandError(err, "", ""))
	}

	if _, err := run(root, "-c", "user.name=gitenv", "-c", "user.email=gitenv@localhost", "commit", "-m", message); err != nil {
		return fmt.Errorf("commit git changes: %w", err)
	}

	if upstream {
		if _, err := run(root, "push"); err != nil {
			return fmt.Errorf("push git repository: %w", err)
		}
		return nil
	}

	if _, err := run(root, "push", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push git repository: %w", err)
	}
	return nil
}

func hasUpstream(root string) bool {
	_, err := run(root, "rev-parse", "--abbrev-ref", "@{upstream}")
	return err == nil
}

func run(dir string, args ...string) (string, error) {
	stdout, stderr, err := command(dir, args...)
	if err != nil {
		return "", commandError(err, stdout, stderr)
	}
	return stdout, nil
}

func command(dir string, args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runWithTimeout(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", commandError(err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func commandError(err error, stdout, stderr string) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = strings.TrimSpace(stdout)
	}
	detail = credentialsInURL.ReplaceAllString(detail, `${1}***@`)
	if detail == "" {
		return err
	}
	return fmt.Errorf("%s: %w", detail, err)
}

func RedactURL(value string) string {
	return credentialsInURL.ReplaceAllString(value, `${1}***@`)
}
