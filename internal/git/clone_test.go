package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneRejectsEmbeddedCredentials proves a credentialed remote is refused
// before any subprocess runs, and that the credentials never leak into the error.
func TestCloneRejectsEmbeddedCredentials(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "clone")
	err := Clone(context.Background(), "https://alice:s3cret@127.0.0.1:1/owner/repo.git", dest)
	if err == nil {
		t.Fatal("Clone() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("Clone() error = %v, want it to name the embedded credentials", err)
	}
	assertNoCredentials(t, err.Error(), "alice", "s3cret")
	// Rejection must precede any spawn: git would have created dest, so its
	// absence proves nothing was launched.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("Clone() touched %s before rejecting; stat err = %v", dest, statErr)
	}
}

// TestCloneRepositoryAttemptOrderSkipsGH covers a github.com identity on a
// machine where gh is absent: the recorded ssh URL is tried first, gh is
// skipped, and the https fallback comes last. PATH is stripped so neither gh
// nor git is found, keeping the test fully offline while still exercising order.
func TestCloneRepositoryAttemptOrderSkipsGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if GHAvailable() {
		t.Fatal("gh unexpectedly available after stripping PATH")
	}
	dest := filepath.Join(t.TempDir(), "vault")
	cloneURL := "git@github.com:owner/repo.git" // the ssh choice the user recorded
	outcome, err := CloneRepository(context.Background(), "github.com/owner/repo", cloneURL, dest)
	if err == nil {
		t.Fatal("CloneRepository() error = nil, want failure with no git present")
	}
	if len(outcome.Attempts) != 2 {
		t.Fatalf("Attempts = %d (%+v), want 2 (recorded then https)", len(outcome.Attempts), outcome.Attempts)
	}
	if outcome.Attempts[0].Method != CloneMethodGit || outcome.Attempts[0].URL != cloneURL {
		t.Fatalf("first attempt = %+v, want recorded ssh URL via git", outcome.Attempts[0])
	}
	if outcome.Attempts[1].Method != CloneMethodGit || outcome.Attempts[1].URL != "https://github.com/owner/repo.git" {
		t.Fatalf("second attempt = %+v, want https fallback via git", outcome.Attempts[1])
	}
	for _, a := range outcome.Attempts {
		if a.Method == CloneMethodGH {
			t.Fatalf("gh attempt recorded despite gh being absent: %+v", a)
		}
	}
}

// TestCloneRepositoryAggregateErrorNamesAllAttempts checks the aggregate error
// names every method and redacted URL tried, so callers can diagnose the cause.
func TestCloneRepositoryAggregateErrorNamesAllAttempts(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dest := filepath.Join(t.TempDir(), "vault")
	cloneURL := "https://mirror.invalid/owner/repo.git"
	outcome, err := CloneRepository(context.Background(), "127.0.0.1:1/owner/repo", cloneURL, dest)
	if err == nil {
		t.Fatal("CloneRepository() error = nil, want aggregate failure")
	}
	if len(outcome.Attempts) != 2 {
		t.Fatalf("Attempts = %d (%+v), want 2", len(outcome.Attempts), outcome.Attempts)
	}
	msg := err.Error()
	for _, want := range []string{cloneURL, "https://127.0.0.1:1/owner/repo.git", string(CloneMethodGit)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("aggregate error %q missing %q", msg, want)
		}
	}
}

// TestCloneRepositoryNeverLeaksCredentials ensures a credentialed cloneURL is
// skipped and that no credential substring appears in the error or outcome.
func TestCloneRepositoryNeverLeaksCredentials(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dest := filepath.Join(t.TempDir(), "vault")
	cloneURL := "https://bob:hunter2@127.0.0.1:1/owner/repo.git"
	outcome, err := CloneRepository(context.Background(), "127.0.0.1:1/owner/repo", cloneURL, dest)
	if err == nil {
		t.Fatal("CloneRepository() error = nil, want failure")
	}
	// The credentialed URL must never be attempted, only the https fallback.
	if len(outcome.Attempts) != 1 || outcome.Attempts[0].URL != "https://127.0.0.1:1/owner/repo.git" {
		t.Fatalf("Attempts = %+v, want only the https fallback (credentialed cloneURL skipped)", outcome.Attempts)
	}
	haystack := []string{err.Error(), outcome.URL, string(outcome.Method)}
	for _, a := range outcome.Attempts {
		haystack = append(haystack, a.URL)
		if a.Err != nil {
			haystack = append(haystack, a.Err.Error())
		}
	}
	for _, h := range haystack {
		assertNoCredentials(t, h, "bob", "hunter2")
	}
}

func assertNoCredentials(t *testing.T, text string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Fatalf("credential %q leaked in %q", s, text)
		}
	}
}
