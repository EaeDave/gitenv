package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// newDivergedVault builds a vault whose local HEAD and remote have each captured
// api/dev to a different value since a shared base, then fetches so the local
// clone sees the divergence. It returns the configured local vault and the local
// HEAD recorded before any resolution runs.
func newDivergedVault(t *testing.T) (vault.LocalConfig, string) {
	t.Helper()
	cfg, root := newVaultForRemote(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=base-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(root, "remote.git")
	initBareRepo(t, bare)
	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "initial")
	if err := gitops.Push(cfg.VaultPath); err != nil {
		t.Fatal(err)
	}

	// Peer clone advances the remote with its own capture of api/dev.
	peerVault := filepath.Join(root, "peer-vault")
	runGitApp(t, root, "clone", bare, peerVault)
	peerProject := filepath.Join(root, "peer-project")
	if err := os.MkdirAll(peerProject, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerProject, ".env"), []byte("API_KEY=remote-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	peerCfg := vault.LocalConfig{VaultPath: peerVault, Projects: map[string]vault.LocalProject{
		"api": {Path: peerProject, ActiveProfile: "dev"},
	}}
	if err := vault.Capture(&peerCfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	commitVault(t, peerVault, "remote capture")
	if err := gitops.Push(peerVault); err != nil {
		t.Fatal(err)
	}

	// Local clone captures a competing value and commits without pushing.
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=local-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "local capture")
	localHead := strings.TrimSpace(runGitApp(t, cfg.VaultPath, "rev-parse", "HEAD"))
	// Let the local clone observe the remote's new commit.
	runGitApp(t, cfg.VaultPath, "fetch", "origin")
	return cfg, localHead
}

func runGitApp(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	} else {
		return string(output)
	}
	return ""
}

func TestDivergenceInspectReportsBothSidesChanged(t *testing.T) {
	cfg, _ := newDivergedVault(t)

	report, err := InspectDivergence(cfg)
	if err != nil {
		t.Fatalf("InspectDivergence() error = %v", err)
	}
	if report.LocalCommits != 1 || report.RemoteCommits != 1 {
		t.Fatalf("commit counts = local %d remote %d, want 1 and 1", report.LocalCommits, report.RemoteCommits)
	}
	conflicted := report.Conflicted()
	if len(conflicted) != 1 {
		t.Fatalf("conflicted profiles = %#v, want exactly one", conflicted)
	}
	got := conflicted[0]
	if got.Project != "api" || got.Profile != "dev" || !got.ChangedLocally || !got.ChangedRemotely {
		t.Fatalf("conflicted profile = %#v, want api/dev changed on both sides", got)
	}
}

func TestDivergenceResolveKeepsLocalAndLeavesRecoverableBackup(t *testing.T) {
	cfg, localHead := newDivergedVault(t)

	backup, err := ResolveDivergence(&cfg, map[string]DivergenceChoice{"api/dev": DivergenceKeepMine})
	if err != nil {
		t.Fatalf("ResolveDivergence() error = %v", err)
	}
	if backup == "" {
		t.Fatal("ResolveDivergence() returned an empty backup ref")
	}

	// The kept version is present, decryptable, and its manifest checksum
	// matches the ciphertext (ReadProfile verifies the checksum internally).
	plaintext, err := vault.ReadProfile(&cfg, "api", "dev")
	if err != nil {
		t.Fatalf("ReadProfile() after resolve error = %v", err)
	}
	if string(plaintext) != "API_KEY=local-value\n" {
		t.Fatalf("resolved profile = %q, want the kept local value", plaintext)
	}
	assertBackupReachable(t, cfg.VaultPath, backup, localHead)
}

func TestDivergenceDiscardTakesRemoteAndLeavesRecoverableBackup(t *testing.T) {
	cfg, localHead := newDivergedVault(t)

	backup, err := DiscardLocalVaultChanges(&cfg)
	if err != nil {
		t.Fatalf("DiscardLocalVaultChanges() error = %v", err)
	}
	if backup == "" {
		t.Fatal("DiscardLocalVaultChanges() returned an empty backup ref")
	}
	plaintext, err := vault.ReadProfile(&cfg, "api", "dev")
	if err != nil {
		t.Fatalf("ReadProfile() after discard error = %v", err)
	}
	if string(plaintext) != "API_KEY=remote-value\n" {
		t.Fatalf("discarded profile = %q, want the remote value", plaintext)
	}
	assertBackupReachable(t, cfg.VaultPath, backup, localHead)
}

func TestDivergenceResolutionsRefuseOnDirtyWorktree(t *testing.T) {
	cfg, _ := newDivergedVault(t)
	if err := os.WriteFile(filepath.Join(cfg.VaultPath, "stray.txt"), []byte("uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDivergence(&cfg, nil); err == nil {
		t.Fatal("ResolveDivergence() on a dirty worktree = nil, want error")
	}
	if _, err := DiscardLocalVaultChanges(&cfg); err == nil {
		t.Fatal("DiscardLocalVaultChanges() on a dirty worktree = nil, want error")
	}
}

// assertBackupReachable proves a user's pre-resolution vault is recoverable: the
// backup ref exists and the HEAD from before the resolution is reachable from it.
func assertBackupReachable(t *testing.T, root, backup, preHead string) {
	t.Helper()
	if got := strings.TrimSpace(runGitApp(t, root, "rev-parse", "--verify", backup)); got == "" {
		t.Fatalf("backup ref %q does not resolve", backup)
	}
	// is-ancestor exits non-zero (fataled by runGitApp) when preHead is not
	// reachable from the backup ref.
	runGitApp(t, root, "merge-base", "--is-ancestor", preHead, backup)
}
