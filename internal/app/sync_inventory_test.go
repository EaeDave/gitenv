package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eaedave/gitenv/internal/envdiff"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func TestInspectSyncWithInventorySeparatesCommittedAndUncommittedChanges(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := []byte("API_KEY=old-secret\n# DEBUG=true\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), base, 0o600); err != nil {
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

	current := []byte("API_KEY=new-secret\nDEBUG=true\nADDED=private-value\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	status, inventory := InspectSyncWithInventory(cfg)
	if status.State != gitops.SyncLocalAhead || !status.Dirty || !inventory.Available {
		t.Fatalf("uncommitted state = %#v inventory=%#v", status, inventory)
	}
	assertInventoryProfileChange(t, inventory.Uncommitted, "api", "dev")
	if !inventory.Committed.Empty() {
		t.Fatalf("unexpected committed changes: %#v", inventory.Committed)
	}
	assertInventoryContainsNoSecrets(t, inventory, "old-secret", "new-secret", "private-value")

	commitVault(t, cfg.VaultPath, "capture api/dev")
	status, inventory = InspectSyncWithInventory(cfg)
	if status.State != gitops.SyncLocalAhead || status.Dirty || status.Ahead != 1 || !inventory.Available {
		t.Fatalf("committed state = %#v inventory=%#v", status, inventory)
	}
	assertInventoryProfileChange(t, inventory.Committed, "api", "dev")
	if !inventory.Uncommitted.Empty() {
		t.Fatalf("unexpected uncommitted changes: %#v", inventory.Uncommitted)
	}
	assertInventoryContainsNoSecrets(t, inventory, "old-secret", "new-secret", "private-value")
}

func TestInspectSyncWithInventoryShowsIncomingRemoteChanges(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_URL=old-secret\n"), 0o600); err != nil {
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

	peerVault := filepath.Join(root, "peer-vault")
	clone := exec.Command("git", "clone", bare, peerVault)
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone peer: %v\n%s", err, output)
	}
	peerProject := filepath.Join(root, "peer-project")
	if err := os.MkdirAll(peerProject, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerProject, ".env"), []byte("API_URL=new-secret\nADDED=remote-value\n"), 0o600); err != nil {
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

	status, inventory := InspectSyncWithInventory(cfg)
	if status.State != gitops.SyncRemoteAhead || status.Behind != 1 || !inventory.Available {
		t.Fatalf("incoming state = %#v inventory=%#v", status, inventory)
	}
	assertInventoryProfileChange(t, inventory.Committed, "api", "dev")
	if !inventory.Uncommitted.Empty() {
		t.Fatalf("unexpected local worktree changes: %#v", inventory.Uncommitted)
	}
	assertInventoryContainsNoSecrets(t, inventory, "old-secret", "new-secret", "remote-value")
	revealed, err := RevealSyncLineDiff(cfg, status, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(revealed.Committed) != 1 {
		t.Fatalf("incoming literal deltas = %#v", revealed.Committed)
	}
	assertLiteralLine(t, revealed.Committed[0].Lines, envdiff.LineRemoved, "API_URL=old-secret")
	assertLiteralLine(t, revealed.Committed[0].Lines, envdiff.LineAdded, "API_URL=new-secret")
	assertLiteralLine(t, revealed.Committed[0].Lines, envdiff.LineAdded, "ADDED=remote-value")
}

func TestInspectSyncWithInventoryIncludesModifiedLocalEnvWhenAnotherEnvIsMissing(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	for _, project := range []string{"api", "missing"} {
		projectDir := filepath.Join(root, project)
		if err := os.MkdirAll(projectDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=old-secret\n# DEBUG=true\nREMOVED=gone\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := vault.Link(&cfg, project, projectDir); err != nil {
			t.Fatal(err)
		}
		if err := vault.Capture(&cfg, project, "dev"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "missing", ".env")); err != nil {
		t.Fatal(err)
	}
	current := []byte("API_KEY=new-secret\nDEBUG=true\nADDED=private-value\n")
	if err := os.WriteFile(filepath.Join(root, "api", ".env"), current, 0o600); err != nil {
		t.Fatal(err)
	}

	_, inventory := InspectSyncWithInventory(cfg)
	if inventory.LocalDetail != "" || len(inventory.LocalEnvs) != 1 {
		t.Fatalf("local env inventory = %#v", inventory)
	}
	local := inventory.LocalEnvs[0]
	if local.Project != "api" || local.Profile != "dev" || local.Diff.Empty() {
		t.Fatalf("local env delta = %#v", local)
	}
	wantKinds := map[string]envdiff.Kind{
		"API_KEY": envdiff.Changed,
		"DEBUG":   envdiff.Enabled,
		"ADDED":   envdiff.Added,
		"REMOVED": envdiff.Removed,
	}
	for _, change := range local.Diff.Changes {
		if wantKinds[change.Key] != change.Kind {
			t.Fatalf("change %s = %s, want %s", change.Key, change.Kind, wantKinds[change.Key])
		}
		delete(wantKinds, change.Key)
	}
	if len(wantKinds) != 0 {
		t.Fatalf("missing local changes: %#v", wantKinds)
	}
	assertInventoryContainsNoSecrets(t, inventory, "old-secret", "new-secret", "private-value")
}

func commitVault(t *testing.T, root, message string) {
	t.Helper()
	commands := [][]string{
		{"add", "--all"},
		{"-c", "user.name=gitenv-test", "-c", "user.email=gitenv@example.test", "commit", "-m", message},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func assertInventoryProfileChange(t *testing.T, delta vault.VaultDelta, project, profile string) {
	t.Helper()
	if len(delta.Profiles) != 1 {
		t.Fatalf("profile deltas = %#v", delta.Profiles)
	}
	got := delta.Profiles[0]
	if got.Project != project || got.Profile != profile || got.Kind != vault.ProfileChanged || got.Diff.Empty() {
		t.Fatalf("profile delta = %#v", got)
	}
}

func assertInventoryContainsNoSecrets(t *testing.T, inventory SyncInventory, secrets ...string) {
	t.Helper()
	dump := fmt.Sprintf("%#v", inventory)
	for _, secret := range secrets {
		if strings.Contains(dump, secret) {
			t.Fatalf("sync inventory exposed %q: %s", secret, dump)
		}
	}
}
