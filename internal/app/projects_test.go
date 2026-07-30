package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// putVaultProject writes a project directly into the vault manifest, so a test
// can set up a vault project (with repositories, profiles, env file) without a
// local link — the fresh-machine situation ProjectStates exists to handle.
func putVaultProject(t *testing.T, cfg vault.LocalConfig, p vault.Project) {
	t.Helper()
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if p.Profiles == nil {
		p.Profiles = map[string]vault.Profile{}
	}
	manifest.Projects[p.Name] = p
	if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
}

// newVaultWithCapturedProject creates a vault, captures env bytes as
// project/profile from a source directory, then drops the local link so the
// project exists only in the vault — ready to be adopted at a new path.
func newVaultWithCapturedProject(t *testing.T, project, profile string, env []byte) (vault.LocalConfig, string) {
	t.Helper()
	cfg, root := newVaultForRemote(t)
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), env, 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := DetectCurrent(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddCurrentProject(&cfg, current, project, profile); err != nil {
		t.Fatalf("AddCurrentProject: %v", err)
	}
	delete(cfg.Projects, project)
	if err := vault.SaveLocal(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

// gitInit initialises a git repository at dir (creating it) so RemoteURL and
// Root have something to read.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "init")
}

func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func findState(states []ProjectState, name string) (ProjectState, bool) {
	for _, state := range states {
		if state.Name == name {
			return state, true
		}
	}
	return ProjectState{}, false
}

func TestProjectStatesReportsUnlinkedVaultProjectAsMissing(t *testing.T) {
	cfg, _ := newVaultForRemote(t)
	putVaultProject(t, cfg, vault.Project{
		Name:         "api",
		Repositories: []vault.Repository{{Identity: "github.com/eaedave/api", CloneURL: "https://github.com/eaedave/api.git"}},
	})
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	states := ProjectStates(cfg, manifest, nil)
	st, ok := findState(states, "api")
	if !ok {
		t.Fatalf("api not present in states: %+v", states)
	}
	if st.Name != "api" {
		t.Fatalf("name = %q", st.Name)
	}
	if st.Kind != ProjectMissing {
		t.Fatalf("kind = %q, want %q", st.Kind, ProjectMissing)
	}
	if st.Identity != "github.com/eaedave/api" {
		t.Fatalf("identity = %q", st.Identity)
	}
}

func TestProjectStatesDemotesLinkedProjectWhenDirectoryVanished(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	putVaultProject(t, cfg, vault.Project{
		Name:         "api",
		Repositories: []vault.Repository{{Identity: "github.com/eaedave/api"}},
	})
	dead := filepath.Join(root, "gone")
	if err := os.MkdirAll(dead, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.Projects["api"] = vault.LocalProject{Path: dead, ActiveProfile: "dev"}
	if err := os.RemoveAll(dead); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	states := ProjectStates(cfg, manifest, map[string]string{"api": "clean"})
	st, ok := findState(states, "api")
	if !ok {
		t.Fatalf("api not present: %+v", states)
	}
	if st.Kind == ProjectLinked {
		t.Fatal("vanished directory still reported as linked")
	}
	if st.Kind != ProjectMissing {
		t.Fatalf("kind = %q, want %q", st.Kind, ProjectMissing)
	}
	if st.Status != "" {
		t.Fatalf("status = %q, want empty for an unlinked project", st.Status)
	}
}

func TestProjectStatesSurfacesEveryDiscoveryCandidate(t *testing.T) {
	cfg, _ := newVaultForRemote(t)
	putVaultProject(t, cfg, vault.Project{
		Name:         "api",
		Repositories: []vault.Repository{{Identity: "github.com/eaedave/api"}},
	})
	want := []string{"/home/a/api", "/home/b/api"}
	cfg.Discovery = &vault.DiscoveryCache{Found: map[string][]string{"github.com/eaedave/api": want}}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	states := ProjectStates(cfg, manifest, nil)
	st, ok := findState(states, "api")
	if !ok {
		t.Fatalf("api not present: %+v", states)
	}
	if st.Kind != ProjectFound {
		t.Fatalf("kind = %q, want %q", st.Kind, ProjectFound)
	}
	if !reflect.DeepEqual(st.Candidates, want) {
		t.Fatalf("candidates = %v, want %v", st.Candidates, want)
	}
}

func TestProjectStatesClassifiesRepolessProjectAsNoRepo(t *testing.T) {
	cfg, _ := newVaultForRemote(t)
	putVaultProject(t, cfg, vault.Project{Name: "local-only"})
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	states := ProjectStates(cfg, manifest, nil)
	st, ok := findState(states, "local-only")
	if !ok {
		t.Fatalf("local-only not present: %+v", states)
	}
	if st.Kind != ProjectNoRepo {
		t.Fatalf("kind = %q, want %q", st.Kind, ProjectNoRepo)
	}
}

func TestMatchVaultProjectsReturnsEverySharedRepositoryProject(t *testing.T) {
	cfg, _ := newVaultForRemote(t)
	const identity = "github.com/acme/mono"
	putVaultProject(t, cfg, vault.Project{Name: "web", Repositories: []vault.Repository{{Identity: identity}}})
	putVaultProject(t, cfg, vault.Project{Name: "api", Repositories: []vault.Repository{{Identity: identity}}})
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	got := MatchVaultProjects(manifest, CurrentProject{RepositoryIdentity: identity})
	want := []string{"api", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchVaultProjects = %v, want %v", got, want)
	}
}

func TestAdoptProjectLinksAndWritesEnv(t *testing.T) {
	cfg, root := newVaultWithCapturedProject(t, "api", "dev", []byte("FROM_VAULT=trusted\n"))
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AdoptProject(&cfg, "api", target, "dev"); err != nil {
		t.Fatalf("AdoptProject: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "FROM_VAULT=trusted\n" {
		t.Fatalf("env = %q, want vault profile", got)
	}
	if cfg.Projects["api"].Path != target {
		t.Fatalf("link path = %q, want %q", cfg.Projects["api"].Path, target)
	}
}

func TestAdoptProjectRefusesUncapturedContentWithoutClobbering(t *testing.T) {
	cfg, root := newVaultWithCapturedProject(t, "api", "dev", []byte("FROM_VAULT=trusted\n"))
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	local := []byte("LOCAL=keepme\n")
	if err := os.WriteFile(filepath.Join(target, ".env"), local, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AdoptProject(&cfg, "api", target, "dev"); err == nil {
		t.Fatal("expected refusal when target holds uncaptured content")
	}
	got, err := os.ReadFile(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(local) {
		t.Fatalf("target env was clobbered: %q", got)
	}
	// The link must persist so the user can resolve the conflict.
	if cfg.Projects["api"].Path != target {
		t.Fatalf("link path = %q, want %q", cfg.Projects["api"].Path, target)
	}
	stored, err := vault.LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Projects["api"].Path != target {
		t.Fatalf("link was not persisted to disk: %+v", stored.Projects["api"])
	}
}

func TestWorkspaceRootInfersCommonParentOfLinkedProjects(t *testing.T) {
	ws := t.TempDir()
	a := filepath.Join(ws, "a")
	b := filepath.Join(ws, "b")
	for _, dir := range []string{a, b} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{
		"a": {Path: a},
		"b": {Path: b},
	}}
	if got := WorkspaceRoot(cfg); got != ws {
		t.Fatalf("WorkspaceRoot = %q, want %q", got, ws)
	}
}

func TestAttachRepositoryRecordsIdentityAddedAfterCapture(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	project := filepath.Join(root, "api")
	gitInit(t, project)
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Captured before the directory has any remote: identity is empty.
	current, err := DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	if current.RepositoryIdentity != "" {
		t.Fatalf("expected no identity before remote, got %q", current.RepositoryIdentity)
	}
	if err := AddCurrentProject(&cfg, current, "api", "dev"); err != nil {
		t.Fatalf("AddCurrentProject: %v", err)
	}
	// User adds the remote afterwards, then attaches it.
	runGitIn(t, project, "remote", "add", "origin", "git@github.com:acme/api.git")
	if err := AttachRepository(cfg, "api"); err != nil {
		t.Fatalf("AttachRepository: %v", err)
	}
	if cfg.Projects["api"].RepositoryIdentity != "github.com/acme/api" {
		t.Fatalf("local identity = %q", cfg.Projects["api"].RepositoryIdentity)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	repos := manifest.Projects["api"].Repositories
	if len(repos) != 1 || repos[0].Identity != "github.com/acme/api" {
		t.Fatalf("repositories = %+v", repos)
	}
	if repos[0].CloneURL != "git@github.com:acme/api.git" {
		t.Fatalf("clone url = %q", repos[0].CloneURL)
	}
}

func TestCredentialedRemoteNeverReachesVaultMetadata(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	project := filepath.Join(root, "api")
	gitInit(t, project)
	runGitIn(t, project, "remote", "add", "origin", "https://user:secret@github.com/acme/api.git")
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	if current.RepositoryIdentity != "github.com/acme/api" {
		t.Fatalf("identity = %q", current.RepositoryIdentity)
	}
	if current.CloneURL != "" {
		t.Fatalf("credentialed clone URL leaked into CurrentProject: %q", current.CloneURL)
	}
	if err := AddCurrentProject(&cfg, current, "api", "dev"); err != nil {
		t.Fatalf("AddCurrentProject: %v", err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	repos := manifest.Projects["api"].Repositories
	if len(repos) != 1 {
		t.Fatalf("repositories = %+v", repos)
	}
	if repos[0].Identity != "github.com/acme/api" {
		t.Fatalf("identity persisted with credentials: %q", repos[0].Identity)
	}
	if repos[0].CloneURL != "" {
		t.Fatalf("credentialed clone URL persisted into vault metadata: %q", repos[0].CloneURL)
	}
}

func TestCloneAndAdoptGuards(t *testing.T) {
	cfg, root := newVaultForRemote(t)

	// A project with no recorded repository cannot be cloned.
	putVaultProject(t, cfg, vault.Project{Name: "norepo"})
	if _, err := CloneAndAdopt(context.Background(), &cfg, "norepo", filepath.Join(root, "dest-norepo"), ""); err == nil {
		t.Fatal("expected refusal for a project with no recorded repository")
	}

	// A non-empty destination is refused before any network work.
	putVaultProject(t, cfg, vault.Project{
		Name:         "api",
		Repositories: []vault.Repository{{Identity: "github.com/acme/api", CloneURL: "https://github.com/acme/api.git"}},
	})
	dest := filepath.Join(root, "dest-nonempty")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := CloneAndAdopt(context.Background(), &cfg, "api", dest, "")
	if err == nil {
		t.Fatal("expected refusal for a non-empty destination")
	}
	if !stringContains(err.Error(), "not empty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "keep")); statErr != nil {
		t.Fatalf("existing content was touched: %v", statErr)
	}
}

// TestCloneAndAdoptReusesCheckoutAfterProfileChoiceFailure covers the exact
// recovery needed when cloning succeeded but adoption stopped for an ambiguous
// profile. A second attempt must reuse the matching checkout, never clone over
// it, and apply the newly selected profile.
func TestCloneAndAdoptReusesCheckoutAfterProfileChoiceFailure(t *testing.T) {
	cfg, root := newVaultWithCapturedProject(t, "api", "dev", []byte("MODE=dev\n"))
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Projects["api"]
	entry.Repositories = []vault.Repository{{Identity: "github.com/acme/api", CloneURL: "https://github.com/acme/api.git"}}
	manifest.Projects["api"] = entry
	if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "Windows Desktop", "api")
	gitInit(t, dest)
	runGitIn(t, dest, "remote", "add", "origin", "https://github.com/acme/api.git")

	outcome, err := CloneAndAdopt(context.Background(), &cfg, "api", dest, "dev")
	if err != nil {
		t.Fatalf("reuse matching checkout: %v", err)
	}
	if outcome.Method != "" {
		t.Fatalf("matching existing checkout was cloned again: %+v", outcome)
	}
	if got := cfg.Projects["api"]; got.Path != dest || got.ActiveProfile != "dev" {
		t.Fatalf("checkout not fully adopted: %+v", got)
	}
	content, err := os.ReadFile(filepath.Join(dest, ".env"))
	if err != nil || string(content) != "MODE=dev\n" {
		t.Fatalf("selected profile not applied: content=%q err=%v", content, err)
	}
}

// TestEnsureVaultUpgradedRefusesWhenRemoteIsAhead pins the guard that keeps two
// computers from upgrading the same vault independently. The upgrade assigns
// fresh random ids, so a second, separate upgrade produces a different layout of
// identical content and the ff-only pull that follows cannot resolve it.
func TestEnsureVaultUpgradedRefusesWhenRemoteIsAhead(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	bare := filepath.Join(root, "remote.git")
	initBareRepo(t, bare)
	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "initial")
	if err := gitops.Push(cfg.VaultPath); err != nil {
		t.Fatal(err)
	}

	// A peer publishes a commit this computer has not pulled.
	peer := filepath.Join(root, "peer")
	runGitIn(t, root, "clone", bare, peer)
	if err := os.WriteFile(filepath.Join(peer, "peer.txt"), []byte("peer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitVault(t, peer, "peer change")
	runGitIn(t, peer, "push", "origin", "HEAD")

	downgradeVaultToV2(t, cfg.VaultPath)

	if _, err := EnsureVaultUpgraded(cfg); err == nil {
		t.Fatal("EnsureVaultUpgraded must refuse while the vault remote is ahead")
	}
	if needed, err := vault.NeedsUpgrade(cfg.VaultPath); err != nil || !needed {
		t.Fatalf("refused upgrade must leave the vault untouched: needed=%v err=%v", needed, err)
	}
}

// TestEnsureVaultUpgradedProceedsWithoutRemote covers the other half of the
// rule: the gate demands evidence of a conflict, so a vault with no remote (and
// by extension an unreachable one) still upgrades. Blocking there would strand
// every offline user for the sake of a rare race.
func TestEnsureVaultUpgradedProceedsWithoutRemote(t *testing.T) {
	cfg, _ := newVaultForRemote(t)
	downgradeVaultToV2(t, cfg.VaultPath)

	changed, err := EnsureVaultUpgraded(cfg)
	if err != nil {
		t.Fatalf("EnsureVaultUpgraded with no remote: %v", err)
	}
	if !changed {
		t.Fatal("upgrade should have run")
	}
	if needed, err := vault.NeedsUpgrade(cfg.VaultPath); err != nil || needed {
		t.Fatalf("vault still reports a pending upgrade: needed=%v err=%v", needed, err)
	}
}

// downgradeVaultToV2 stamps the bootstrap file back to version 2 so the upgrade
// path becomes pending again. The project layout is irrelevant here: these tests
// exercise the gate, not the migration itself.
func downgradeVaultToV2(t *testing.T, vaultPath string) {
	t.Helper()
	path := filepath.Join(vaultPath, "gitenv.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["version"] = 2
	raw["projects"] = map[string]any{}
	updated, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(updated, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
