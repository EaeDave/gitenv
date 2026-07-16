package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eaedave/gitenv/internal/envdiff"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func TestRevealSyncLineDiffReturnsLiteralLocalLinesOnlyOnExplicitCall(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	projectDir := filepath.Join(root, "api")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := []byte("API_KEY=old-secret\nDEBUG=false\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), base, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	current := []byte("API_KEY=new-secret\nDEBUG=false\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), current, 0o600); err != nil {
		t.Fatal(err)
	}

	diff, err := RevealSyncLineDiff(cfg, gitops.SyncStatus{State: gitops.SyncNoRemote})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.LocalEnvs) != 1 {
		t.Fatalf("local line deltas = %#v", diff.LocalEnvs)
	}
	lines := diff.LocalEnvs[0].Lines
	assertLiteralLine(t, lines, envdiff.LineRemoved, "API_KEY=old-secret")
	assertLiteralLine(t, lines, envdiff.LineAdded, "API_KEY=new-secret")
	assertLiteralLine(t, lines, envdiff.LineContext, "DEBUG=false")
}

func assertLiteralLine(t *testing.T, lines []envdiff.LineChange, kind envdiff.LineKind, text string) {
	t.Helper()
	for _, line := range lines {
		if line.Kind == kind && line.Text == text {
			return
		}
	}
	t.Fatalf("missing %s line %q in %#v", kind, text, lines)
}
