package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CloneMethod string

const (
	CloneMethodGit CloneMethod = "git"
	CloneMethodGH  CloneMethod = "gh"
)

type CloneAttempt struct {
	Method CloneMethod
	URL    string // redacted, safe to display
	Err    error  // nil on the attempt that succeeded
}

type CloneOutcome struct {
	Method   CloneMethod
	URL      string // redacted
	Attempts []CloneAttempt
}

// CloneRepository clones identity (canonical "host[:port]/path") into dest,
// trying methods in the order a freshly formatted machine is most likely to
// have working credentials for:
//
//  1. the recorded cloneURL, when non-empty and credential-free — it preserves
//     the ssh-vs-https choice the user originally made;
//  2. `gh repo clone <owner>/<repo>`, when the host is exactly github.com and
//     gh is installed AND authenticated — the only path that clones a private
//     repo on a machine with no ssh key;
//  3. https://<identity>.git.
//
// Step 1 is skipped when it would duplicate step 3. It returns the first
// success. Every attempt is recorded in Outcome.Attempts with a redacted URL;
// when all fail the returned error names each method and its redacted cause so
// callers can tell "private repo, no credentials" from "host unreachable".
func CloneRepository(ctx context.Context, identity, cloneURL, dest string) (CloneOutcome, error) {
	var outcome CloneOutcome

	absDest, err := filepath.Abs(dest)
	if err != nil {
		return outcome, fmt.Errorf("resolve clone destination: %w", err)
	}
	// Only a directory this call creates may be swept between attempts; a
	// pre-existing dest belongs to the user and must survive.
	preexisting := pathExists(absDest)

	httpsURL := "https://" + strings.TrimSpace(identity) + ".git"

	try := func(method CloneMethod, displayURL string, run func() error) bool {
		attemptErr := run()
		redacted := RedactURL(displayURL)
		outcome.Attempts = append(outcome.Attempts, CloneAttempt{
			Method: method,
			URL:    redacted,
			Err:    attemptErr,
		})
		if attemptErr == nil {
			outcome.Method = method
			outcome.URL = redacted
			return true
		}
		// Remove the husk git may have left so the next method does not fail
		// with "destination already exists"; never touch a pre-existing dir.
		if !preexisting {
			_ = os.RemoveAll(absDest)
		}
		return false
	}

	recorded := strings.TrimSpace(cloneURL)
	if recorded != "" && !embeddedCredential.MatchString(recorded) && recorded != httpsURL {
		if try(CloneMethodGit, recorded, func() error { return Clone(ctx, recorded, absDest) }) {
			return outcome, nil
		}
	}

	if host, ownerRepo, ok := githubOwnerRepo(identity); ok && GHAvailable() && GHAuthenticated(ctx, host) {
		if try(CloneMethodGH, httpsURL, func() error { return ghClone(ctx, ownerRepo, absDest) }) {
			return outcome, nil
		}
	}

	if try(CloneMethodGit, httpsURL, func() error { return Clone(ctx, httpsURL, absDest) }) {
		return outcome, nil
	}

	return outcome, aggregateCloneError(outcome.Attempts)
}

// githubOwnerRepo splits a canonical identity into its host and owner/repo path
// when — and only when — the host is exactly github.com, the sole host gh clones.
func githubOwnerRepo(identity string) (host, ownerRepo string, ok bool) {
	slash := strings.IndexByte(identity, '/')
	if slash <= 0 {
		return "", "", false
	}
	host = identity[:slash]
	ownerRepo = identity[slash+1:]
	if host != "github.com" || ownerRepo == "" {
		return "", "", false
	}
	return host, ownerRepo, true
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// aggregateCloneError names every method tried and its redacted cause so the
// TUI can distinguish "private repo, no credentials" from "host unreachable".
func aggregateCloneError(attempts []CloneAttempt) error {
	if len(attempts) == 0 {
		return errors.New("clone repository: no clone method was available")
	}
	parts := make([]string, 0, len(attempts))
	for _, a := range attempts {
		parts = append(parts, fmt.Sprintf("%s %s: %v", a.Method, a.URL, a.Err))
	}
	return fmt.Errorf("clone repository failed after %d attempt(s): %s", len(attempts), RedactURL(strings.Join(parts, "; ")))
}
