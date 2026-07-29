package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// ProjectStateKind classifies a project by what a machine can do with it right
// now: apply its env (linked), link a clone found on disk (found), clone the
// recorded repository (missing), or nothing automatic (no-repo).
type ProjectStateKind string

const (
	ProjectLinked  ProjectStateKind = "linked"
	ProjectFound   ProjectStateKind = "found"
	ProjectMissing ProjectStateKind = "missing"
	ProjectNoRepo  ProjectStateKind = "no-repo"
)

// ProjectState is a single row of the projects screen: everything the TUI and
// CLI need to render a project and offer the right next action, computed without
// touching the network and with at most an os.Stat per linked path.
type ProjectState struct {
	Name          string
	Kind          ProjectStateKind
	Path          string   // set when linked
	Candidates    []string // discovered paths when Kind == ProjectFound
	Identity      string
	CloneURL      string
	ActiveProfile string
	EnvFile       string
	LineEndings   vault.LineEndingPolicy
	Status        string // vault.Status result, only when linked
}

// ProjectStates returns one ProjectState per project in the union of the vault
// manifest and the local config, sorted by name. It is the fresh-machine fix:
// a formatted computer sees every vault project, not just the handful still
// linked in config.json.
//
// It must stay cheap and offline because the TUI calls it on every reload: the
// only filesystem access is an os.Stat on each linked path (to demote a link
// whose directory has vanished), and discovery candidates come from the cached
// scan in cfg.Discovery, never a fresh walk.
func ProjectStates(cfg vault.LocalConfig, manifest vault.Manifest, statuses map[string]string) []ProjectState {
	names := make(map[string]struct{}, len(manifest.Projects)+len(cfg.Projects))
	for name := range manifest.Projects {
		names[name] = struct{}{}
	}
	for name := range cfg.Projects {
		names[name] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	states := make([]ProjectState, 0, len(sorted))
	for _, name := range sorted {
		entry := manifest.Projects[name] // zero Project{} when local-only
		local, hasLocal := cfg.Projects[name]

		identity, cloneURL := preferredRepository(entry.Repositories)
		state := ProjectState{
			Name:          name,
			Identity:      identity,
			CloneURL:      cloneURL,
			ActiveProfile: local.ActiveProfile,
			EnvFile:       vault.EnvFileOf(entry),
			LineEndings:   entry.LineEndings,
		}

		switch {
		case hasLocal && local.Path != "" && dirExists(local.Path):
			// A dead path is deliberately NOT reported as linked: a formatted
			// machine keeps a stale config.json, and calling a vanished
			// directory "linked" is precisely the confusing state being removed.
			state.Kind = ProjectLinked
			state.Path = local.Path
			state.Status = statuses[name]
		default:
			candidates := discoveryCandidates(cfg, identity)
			switch {
			case len(candidates) > 0:
				state.Kind = ProjectFound
				state.Candidates = candidates
			case len(entry.Repositories) > 0:
				state.Kind = ProjectMissing
			default:
				state.Kind = ProjectNoRepo
			}
		}
		states = append(states, state)
	}
	return states
}

// dirExists reports whether path names an existing directory. It is the only
// filesystem call ProjectStates is allowed to make.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// preferredRepository picks the identity and clone URL a missing project should
// be cloned from: the first recorded repository, but preferring one that
// carries a clone URL, since the canonical identity alone is lower-cased and
// scheme-less and therefore not always cloneable.
func preferredRepository(repos []vault.Repository) (identity, cloneURL string) {
	if len(repos) == 0 {
		return "", ""
	}
	best := repos[0]
	for _, repo := range repos {
		if repo.CloneURL != "" {
			best = repo
			break
		}
	}
	return best.Identity, best.CloneURL
}

// discoveryCandidates returns a defensive copy of the cached discovery paths for
// an identity, so a caller cannot mutate the config's slice.
func discoveryCandidates(cfg vault.LocalConfig, identity string) []string {
	if identity == "" || cfg.Discovery == nil {
		return nil
	}
	paths := cfg.Discovery.Found[identity]
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, len(paths))
	copy(out, paths)
	return out
}

// WorkspaceRoot returns where clones of missing projects should be created.
// The configured WorkspaceRoot wins; otherwise it is inferred, which is what
// makes the clone destination correct without any configuration on a machine
// that already has at least one project linked.
func WorkspaceRoot(cfg vault.LocalConfig) string {
	if root := strings.TrimSpace(cfg.WorkspaceRoot); root != "" {
		return root
	}
	// Infer from the parents of currently linked project paths: the most common
	// parent directory, ties broken by the lexicographically smallest parent so
	// the answer is deterministic regardless of map iteration order.
	counts := map[string]int{}
	for _, local := range cfg.Projects {
		if local.Path == "" {
			continue
		}
		counts[filepath.Dir(local.Path)]++
	}
	if len(counts) > 0 {
		best, bestCount := "", -1
		for parent, count := range counts {
			if count > bestCount || (count == bestCount && parent < best) {
				best, bestCount = parent, count
			}
		}
		return best
	}
	// No linked projects to learn from: the first existing conventional
	// workspace directory, falling back to the home directory itself.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return home
	}
	for _, name := range []string{"dev", "src", "code", "projects", "repos"} {
		candidate := filepath.Join(home, name)
		if dirExists(candidate) {
			return candidate
		}
	}
	return home
}

// SuggestCloneDest proposes where to clone a project: WorkspaceRoot joined with
// the repository name (the last element of the recorded identity) so the created
// directory matches what `git clone` would produce, falling back to the project
// name when no identity is recorded locally.
func SuggestCloneDest(cfg vault.LocalConfig, name string) string {
	base := name
	if local, ok := cfg.Projects[name]; ok {
		if repo := repoNameFromIdentity(local.RepositoryIdentity); repo != "" {
			base = repo
		}
	}
	return filepath.Join(WorkspaceRoot(cfg), base)
}

// repoNameFromIdentity returns the last path element of a canonical identity
// (host/owner/repo -> repo), or "" when the identity has no path element.
func repoNameFromIdentity(identity string) string {
	identity = strings.TrimRight(strings.TrimSpace(identity), "/")
	if idx := strings.LastIndexByte(identity, '/'); idx >= 0 {
		return identity[idx+1:]
	}
	return ""
}

// AdoptProject links an existing directory to a vault project and applies the
// selected profile in one step. The apply is non-forced on purpose: if the
// directory already holds an env file with uncaptured content, adoption fails
// with that error instead of overwriting the user's file, and the TUI turns the
// failure into a diff prompt. The link is persisted before the apply so the user
// can resolve the conflict against an already-linked project.
func AdoptProject(cfg *vault.LocalConfig, name, path, profile string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("adopt %q: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("adopt %q: %s is not a directory", name, path)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	entry, ok := manifest.Projects[name]
	if !ok {
		return fmt.Errorf("project %q does not exist in vault", name)
	}
	// Resolve the profile before linking so a bad or ambiguous choice fails
	// without leaving a half-adopted project behind.
	resolved, err := resolveAdoptProfile(*cfg, entry, name, profile)
	if err != nil {
		return err
	}
	if err := vault.Link(cfg, name, path); err != nil {
		return err
	}
	// Record the repository identity from the directory's actual origin remote
	// when it has one, so a later scan-free reload can classify siblings and
	// another machine can clone it.
	local := cfg.Projects[name]
	if rawURL, urlErr := gitops.RemoteURL(local.Path, "origin"); urlErr == nil {
		if identity := gitops.NormalizeRemoteURL(rawURL); identity != "" {
			local.RepositoryIdentity = identity
			cfg.Projects[name] = local
			cloneURL := ""
			if !credentialedURL.MatchString(rawURL) {
				cloneURL = rawURL
			}
			entry.Repositories = appendRepository(entry.Repositories, vault.Repository{Identity: identity, CloneURL: cloneURL})
			manifest.Projects[name] = entry
			if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
				return err
			}
		}
	}
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
	}
	return vault.Apply(cfg, name, resolved, false)
}

// resolveAdoptProfile picks the profile AdoptProject applies. An explicit name
// is used as-is (Apply validates its existence). When empty, a single-profile
// project needs no choice; otherwise the locally recorded active profile is
// used, and failing that the caller must choose, so the available names are
// listed in the error.
func resolveAdoptProfile(cfg vault.LocalConfig, entry vault.Project, name, profile string) (string, error) {
	if strings.TrimSpace(profile) != "" {
		return profile, nil
	}
	if len(entry.Profiles) == 0 {
		return "", fmt.Errorf("project %q has no profiles to apply", name)
	}
	if len(entry.Profiles) == 1 {
		for only := range entry.Profiles {
			return only, nil
		}
	}
	if active := cfg.Projects[name].ActiveProfile; active != "" {
		if _, ok := entry.Profiles[active]; ok {
			return active, nil
		}
	}
	return "", fmt.Errorf("project %q has multiple profiles; choose one of: %s", name, strings.Join(sortedProfileNames(entry.Profiles), ", "))
}

// sortedProfileNames returns a project's profile names in a stable order for
// user-facing error messages.
func sortedProfileNames(profiles map[string]vault.Profile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CloneAndAdopt clones a missing project's recorded repository into dest and
// then adopts it. On adopt failure it returns the clone outcome alongside the
// error so the UI can say "cloned, but the env file needs attention" rather than
// implying the clone failed.
func CloneAndAdopt(ctx context.Context, cfg *vault.LocalConfig, name, dest, profile string) (gitops.CloneOutcome, error) {
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return gitops.CloneOutcome{}, err
	}
	entry, ok := manifest.Projects[name]
	if !ok {
		return gitops.CloneOutcome{}, fmt.Errorf("project %q does not exist in vault", name)
	}
	identity, cloneURL := preferredRepository(entry.Repositories)
	if identity == "" && cloneURL == "" {
		return gitops.CloneOutcome{}, fmt.Errorf("project %q has no recorded repository to clone", name)
	}
	if err := ensureEmptyDest(dest); err != nil {
		return gitops.CloneOutcome{}, err
	}
	outcome, err := gitops.CloneRepository(ctx, identity, cloneURL, dest)
	if err != nil {
		return outcome, err
	}
	if err := AdoptProject(cfg, name, dest, profile); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// ensureEmptyDest refuses a clone destination that already exists and is a
// non-empty directory (git clone would fail anyway, but a clear message beats a
// cryptic subprocess error and protects an existing checkout). A missing dest or
// an existing empty directory is fine.
func ensureEmptyDest(dest string) error {
	entries, err := os.ReadDir(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("clone destination %s: %w", dest, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("clone destination %s already exists and is not empty", dest)
	}
	return nil
}

// RunDiscovery scans the default roots for clones, folds them into an
// identity -> sorted-unique-paths map, and caches the result in cfg.Discovery.
// It is cached because a scan is expensive and must never run unprompted on
// launch: the reload path reads this cache, never the filesystem.
func RunDiscovery(ctx context.Context, cfg *vault.LocalConfig) (map[string][]string, error) {
	roots := DefaultScanRoots()
	candidates, err := ScanRepositories(ctx, ScanOptions{Roots: roots})
	if err != nil {
		return nil, err
	}
	found := foldCandidates(candidates)
	if cfg.Discovery == nil {
		cfg.Discovery = &vault.DiscoveryCache{}
	}
	cfg.Discovery.ScannedAt = time.Now().UTC()
	cfg.Discovery.Roots = roots
	cfg.Discovery.Found = found
	if err := vault.SaveLocal(*cfg); err != nil {
		return nil, err
	}
	return found, nil
}

// foldCandidates groups scan candidates by canonical identity into sorted,
// de-duplicated path lists. Candidates without an identity are dropped: they can
// never be matched to a vault project.
func foldCandidates(candidates []Candidate) map[string][]string {
	byIdentity := map[string]map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate.Identity == "" {
			continue
		}
		set := byIdentity[candidate.Identity]
		if set == nil {
			set = map[string]struct{}{}
			byIdentity[candidate.Identity] = set
		}
		set[candidate.Path] = struct{}{}
	}
	found := make(map[string][]string, len(byIdentity))
	for identity, set := range byIdentity {
		paths := make([]string, 0, len(set))
		for path := range set {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		found[identity] = paths
	}
	return found
}

// SetProjectEnvFile records the project-relative env file on a project's vault
// metadata, after normalizing it.
func SetProjectEnvFile(cfg vault.LocalConfig, name, envFile string) error {
	normalized, err := vault.NormalizeEnvFile(envFile)
	if err != nil {
		return err
	}
	return updateProjectMetadata(cfg, name, func(entry *vault.Project) {
		entry.EnvFile = normalized
	})
}

// SetProjectLineEndings records a project's line-ending policy on its vault
// metadata, after validating it.
func SetProjectLineEndings(cfg vault.LocalConfig, name string, policy vault.LineEndingPolicy) error {
	if err := vault.ValidateLineEndingPolicy(policy); err != nil {
		return err
	}
	return updateProjectMetadata(cfg, name, func(entry *vault.Project) {
		entry.LineEndings = policy
	})
}

// updateProjectMetadata loads the manifest, applies mutate to the named
// project's entry, and persists it.
//
// The entry is created when it does not exist yet, provided the project is
// linked on this computer. That ordering is required, not lenient: `gitenv link
// --env-file apps/web/.env` has to record the managed path *before* the first
// capture, because capture reads whatever path the metadata names. Rejecting an
// uncaptured project here would silently leave the default ".env" in effect.
// A name that is neither in the vault nor linked locally is a typo and is
// rejected.
//
// The sealed check comes first because a sealed manifest has no readable
// projects, which would otherwise masquerade as an unknown-project error.
func updateProjectMetadata(cfg vault.LocalConfig, name string, mutate func(*vault.Project)) error {
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	if manifest.Sealed {
		return errors.New("unlock the vault first: its metadata is encrypted and unreadable")
	}
	entry, ok := manifest.Projects[name]
	if !ok {
		if _, linked := cfg.Projects[name]; !linked {
			return fmt.Errorf("project %q does not exist in vault and is not linked on this computer", name)
		}
		if _, err := manifest.EnsureProjectID(name); err != nil {
			return err
		}
		entry = manifest.Projects[name]
	}
	mutate(&entry)
	manifest.Projects[name] = entry
	return vault.SaveManifest(cfg.VaultPath, manifest)
}

// EnsureVaultUpgraded runs the v2->v3 metadata upgrade when a vault is
// configured. Both the TUI and the CLI call it right after resolving the vault
// path and before reading the manifest.
func EnsureVaultUpgraded(cfg vault.LocalConfig) (bool, error) {
	if strings.TrimSpace(cfg.VaultPath) == "" {
		return false, nil
	}
	return vault.UpgradeManifest(cfg.VaultPath)
}
