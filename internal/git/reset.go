package git

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// BackupRef creates a ref pointing at the current HEAD so a destructive
// operation stays recoverable, returning the ref name it created.
//
// The ref is named refs/heads/<prefix>/<UTC timestamp> (for example
// gitenv-backup/20260730-143210). It never overwrites an existing ref: if that
// second-resolution name is already taken, a numeric suffix is appended until a
// free name is found, so resolving a vault twice in the same second can never
// clobber the first backup. The plain branch name is returned so the UI can
// tell the user exactly where their old vault went.
func BackupRef(root, prefix string) (string, error) {
	base := prefix + "/" + time.Now().UTC().Format("20060102-150405")
	name := base
	for attempt := 2; refExists(root, name); attempt++ {
		name = fmt.Sprintf("%s-%d", base, attempt)
	}
	if _, err := run(root, "branch", name); err != nil {
		return "", fmt.Errorf("create backup ref: %w", err)
	}
	return name, nil
}

// refExists reports whether refs/heads/<name> already resolves.
func refExists(root, name string) bool {
	_, _, err := command(root, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// ResetHard moves the current branch and worktree to revision.
func ResetHard(root, revision string) error {
	if _, err := run(root, "reset", "--hard", revision); err != nil {
		return fmt.Errorf("rebuild vault worktree: %w", err)
	}
	return nil
}

// CommitAll stages everything and commits with gitenv's own identity. It is a
// no-op (returning nil) when nothing is staged, so a resolution that ends up
// matching the remote exactly does not fail on an empty commit. The explicit
// -c identity mirrors CommitAndPush: publishing must never depend on the user's
// global git config being set.
func CommitAll(root, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("commit message must not be empty")
	}
	if _, err := run(root, "add", "--all"); err != nil {
		return fmt.Errorf("stage git changes: %w", err)
	}
	if _, _, err := command(root, "diff", "--cached", "--quiet"); err == nil {
		return nil // nothing to commit
	}
	if _, err := run(root, "-c", "user.name=gitenv", "-c", "user.email=gitenv@localhost", "commit", "-m", message); err != nil {
		return fmt.Errorf("commit git changes: %w", err)
	}
	return nil
}

// MergeBase returns the best common ancestor of two revisions.
func MergeBase(root, a, b string) (string, error) {
	output, err := run(root, "merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("find common ancestor: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// AheadBehind reports how many commits HEAD is ahead of and behind its
// upstream, i.e. the local-only and remote-only commit counts of a divergence.
func AheadBehind(root string) (int, int, error) {
	return countAheadBehind(root)
}
