package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

type CurrentProject struct {
	Path       string
	Name       string
	HasEnv     bool
	LinkedName string
	// RepositoryIdentity is the canonical comparison key derived from the
	// origin remote (git.NormalizeRemoteURL).
	RepositoryIdentity string
	// CloneURL is the credential-free origin remote URL exactly as configured,
	// kept so another machine can clone this repository later. The canonical
	// identity is lower-cased and scheme-less and is not always cloneable.
	CloneURL string
}

func CreateVault(cfg *vault.LocalConfig, root, recoveryPath, remoteURL string) error {
	absolute, err := filepath.Abs(expandHome(root))
	if err != nil {
		return err
	}
	identity, err := vault.LoadIdentity()
	if err != nil {
		identity, err = vault.GenerateIdentity()
		if err != nil {
			return err
		}
		if err := vault.SaveIdentity(identity); err != nil {
			return err
		}
	}
	if err := vault.Init(absolute, identity.Recipient().String()); err != nil {
		return err
	}
	if err := gitops.Init(absolute); err != nil {
		return err
	}
	cfg.VaultPath = absolute
	if cfg.Projects == nil {
		cfg.Projects = map[string]vault.LocalProject{}
	}
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
	}
	if strings.TrimSpace(recoveryPath) != "" {
		if err := ExportIdentity(recoveryPath); err != nil {
			return err
		}
	}
	if strings.TrimSpace(remoteURL) != "" {
		if err := gitops.AddRemote(absolute, "origin", remoteURL); err != nil {
			return err
		}
	}
	return nil
}

func CloneVault(cfg *vault.LocalConfig, remoteURL, root, recoveryPath string) error {
	if strings.TrimSpace(recoveryPath) == "" {
		return errors.New("recovery identity path is required")
	}
	absolute, err := filepath.Abs(expandHome(root))
	if err != nil {
		return err
	}
	if err := gitops.Clone(context.Background(), remoteURL, absolute); err != nil {
		return err
	}
	if _, err := vault.LoadManifest(absolute); err != nil {
		return fmt.Errorf("cloned repository is not a gitenv vault: %w", err)
	}
	if err := ImportIdentity(recoveryPath); err != nil {
		return err
	}
	cfg.VaultPath = absolute
	if cfg.Projects == nil {
		cfg.Projects = map[string]vault.LocalProject{}
	}
	return vault.SaveLocal(*cfg)
}

// ExportRecoveryKey writes the vault's recovery identity to target and records
// that this computer has confirmed a backup, so the interface stops warning
// about it. Callers that only need the bytes on disk use ExportIdentity.
func ExportRecoveryKey(cfg *vault.LocalConfig, target string) error {
	if err := ExportIdentity(target); err != nil {
		return err
	}
	return confirmRecoveryBackup(cfg)
}

// ImportRecoveryKey stores a pasted recovery identity and records that this
// computer has confirmed a backup: a user who just typed the key in demonstrably
// holds a copy, so nagging them to save one would be a false alarm.
func ImportRecoveryKey(cfg *vault.LocalConfig, value string) error {
	if err := ImportIdentityValue(value); err != nil {
		return err
	}
	return confirmRecoveryBackup(cfg)
}

func confirmRecoveryBackup(cfg *vault.LocalConfig) error {
	now := time.Now().UTC()
	cfg.RecoveryExportedAt = &now
	return vault.SaveLocal(*cfg)
}

// HasRecoveryBackup reports whether this computer has ever confirmed that a
// recovery key exists, by exporting or importing one.
func HasRecoveryBackup(cfg vault.LocalConfig) bool {
	return cfg.RecoveryExportedAt != nil
}

func ExportIdentity(target string) error {
	absolute, err := filepath.Abs(expandHome(target))
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolute); err == nil {
		return fmt.Errorf("refusing to overwrite %s", absolute)
	}
	identity, err := vault.LoadIdentity()
	if err != nil {
		return err
	}
	return vault.WriteAtomic(absolute, []byte(identity.String()+"\n"), 0o600)
}

func ImportIdentity(source string) error {
	absolute, err := filepath.Abs(expandHome(source))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return err
	}
	return ImportIdentityValue(string(data))
}

// ImportIdentityValue validates and stores a pasted age recovery identity.
func ImportIdentityValue(value string) error {
	identity, err := vault.ParseIdentity([]byte(strings.TrimSpace(value)))
	if err != nil {
		return fmt.Errorf("invalid recovery identity: %w", err)
	}
	if err := vault.StoreUnlockedIdentity(identity, vault.StoreIdentityFallback); err != nil {
		return err
	}
	_ = vault.SaveIdentityToKeychain(identity)
	return nil
}

// DisconnectVault forgets only this computer's vault and project mappings.
// It never removes the vault directory or its Git remote.
func DisconnectVault(cfg *vault.LocalConfig) error {
	if cfg == nil {
		return errors.New("local config is required")
	}
	*cfg = vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	return vault.SaveLocal(*cfg)
}

func DetectCurrent(cfg vault.LocalConfig, cwd string) (CurrentProject, error) {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return CurrentProject{}, err
	}
	if gitRoot, gitErr := gitops.Root(absolute); gitErr == nil {
		absolute = gitRoot
	}
	current := CurrentProject{Path: absolute, Name: filepath.Base(absolute)}
	// Populate canonical repository identity from the application repo's origin
	// remote (best-effort: empty when the directory has no git remote).
	if rawURL, urlErr := gitops.RemoteURL(absolute, "origin"); urlErr == nil {
		current.RepositoryIdentity = gitops.NormalizeRemoteURL(rawURL)
		// Record the exact remote URL so another machine can clone it later,
		// but only when it is credential-free: invariant 7 forbids a
		// credentialed URL ever reaching vault metadata, which is committed to
		// the vault repo.
		if !credentialedURL.MatchString(rawURL) {
			current.CloneURL = rawURL
		}
	}
	if _, err := os.Stat(filepath.Join(absolute, ".env")); err == nil {
		current.HasEnv = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return CurrentProject{}, err
	}
	for name, project := range cfg.Projects {
		if samePath(project.Path, absolute) {
			current.LinkedName = name
			break
		}
		// Exact remote identity match auto-links even when the local path differs.
		if current.RepositoryIdentity != "" && current.RepositoryIdentity == project.RepositoryIdentity {
			current.LinkedName = name
			break
		}
	}
	return current, nil
}

// MatchVaultProjects returns the names of every vault project whose recorded
// repositories include the current directory's identity, sorted by name. It is
// plural because a monorepo is modelled as several gitenv projects sharing one
// repository identity (web -> apps/web/.env, api -> apps/api/.env): cloning the
// repository once must be able to adopt all of them, so returning only the
// first would silently strip the rest.
func MatchVaultProjects(manifest vault.Manifest, current CurrentProject) []string {
	if current.RepositoryIdentity == "" {
		return nil
	}
	var names []string
	for name, project := range manifest.Projects {
		for _, repository := range project.Repositories {
			if repository.Identity == current.RepositoryIdentity {
				names = append(names, name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

func AddCurrentProject(cfg *vault.LocalConfig, current CurrentProject, name, profile string) error {
	if !current.HasEnv {
		return errors.New("current directory has no .env file")
	}
	if err := vault.Link(cfg, name, current.Path); err != nil {
		return err
	}
	// Persist the canonical repository identity so future DetectCurrent calls
	// can auto-link clones at different local paths.
	if current.RepositoryIdentity != "" {
		lp := cfg.Projects[name]
		lp.RepositoryIdentity = current.RepositoryIdentity
		cfg.Projects[name] = lp
	}
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
	}
	if current.RepositoryIdentity != "" {
		manifest, err := vault.LoadManifest(cfg.VaultPath)
		if err != nil {
			return err
		}
		entry := manifest.Projects[name]
		entry.Repositories = appendRepository(entry.Repositories, vault.Repository{Identity: current.RepositoryIdentity, CloneURL: current.CloneURL})
		manifest.Projects[name] = entry
		if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
			return err
		}
	}
	return vault.Capture(cfg, name, profile)
}

func LinkExistingProject(cfg *vault.LocalConfig, current CurrentProject, name, profile string) error {
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	project, exists := manifest.Projects[name]
	if !exists {
		return fmt.Errorf("project %q does not exist in vault", name)
	}
	if _, exists := project.Profiles[profile]; !exists {
		return fmt.Errorf("profile %q does not exist for project %q", profile, name)
	}
	if err := vault.Link(cfg, name, current.Path); err != nil {
		return err
	}
	if current.RepositoryIdentity != "" {
		local := cfg.Projects[name]
		local.RepositoryIdentity = current.RepositoryIdentity
		cfg.Projects[name] = local
		project.Repositories = appendRepository(project.Repositories, vault.Repository{Identity: current.RepositoryIdentity, CloneURL: current.CloneURL})
		manifest.Projects[name] = project
		if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
			return err
		}
	}
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
	}
	return vault.Apply(cfg, name, profile, true)
}

// AttachRepository re-reads a linked project's current origin remote and records
// it in both the local config and the vault metadata. A project first captured
// in a directory with no git remote has an empty identity and is otherwise
// undiscoverable and uncloneable forever (gap #3); calling this the moment the
// user adds a remote gives the project its identity without re-capturing.
//
// It fails when there is nothing to record, which is what an explicit user
// request wants. Callers recording opportunistically want TryAttachRepository.
func AttachRepository(cfg vault.LocalConfig, name string) error {
	recorded, err := TryAttachRepository(cfg, name)
	if err != nil {
		return err
	}
	if !recorded {
		return fmt.Errorf("project %q directory has no recognizable origin remote", name)
	}
	return nil
}

// TryAttachRepository records a linked project's origin remote when there is one
// to record, reporting whether it did. A directory with no origin, or one whose
// remote is not a recognizable Git URL, is not an error: linking a project that
// has no remote yet is legitimate and must not fail the link.
func TryAttachRepository(cfg vault.LocalConfig, name string) (bool, error) {
	local, ok := cfg.Projects[name]
	if !ok || local.Path == "" {
		return false, fmt.Errorf("project %q is not linked on this computer", name)
	}
	rawURL, err := gitops.RemoteURL(local.Path, "origin")
	if err != nil {
		return false, nil
	}
	identity := gitops.NormalizeRemoteURL(rawURL)
	if identity == "" {
		return false, nil
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return false, err
	}
	if manifest.Sealed {
		return false, errors.New("unlock the vault first: its metadata is encrypted and unreadable")
	}
	// The vault entry may not exist yet: `gitenv link` records the repository
	// before the first capture, and that is the point — the identity is what
	// makes the project discoverable on the next machine.
	if _, exists := manifest.Projects[name]; !exists {
		if _, err := manifest.EnsureProjectID(name); err != nil {
			return false, err
		}
	}
	entry := manifest.Projects[name]
	// Only persist a credential-free clone URL (invariant 7); the canonical
	// identity is always safe to store.
	cloneURL := ""
	if !credentialedURL.MatchString(rawURL) {
		cloneURL = rawURL
	}
	entry.Repositories = appendRepository(entry.Repositories, vault.Repository{Identity: identity, CloneURL: cloneURL})
	manifest.Projects[name] = entry
	if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
		return false, err
	}
	local.RepositoryIdentity = identity
	cfg.Projects[name] = local
	return true, vault.SaveLocal(cfg)
}

func AddRemote(cfg vault.LocalConfig, remoteURL string) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	return gitops.AddRemote(cfg.VaultPath, "origin", remoteURL)
}

// credentialedURL matches a remote URL whose userinfo carries a secret
// (scheme://user:pass@host). git.embeddedCredential performs the identical
// check but is unexported, so it is duplicated here: invariant 7 forbids a
// credentialed URL ever being persisted into vault metadata, which is committed
// to the vault repo.
var credentialedURL = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://[^/@\s]*:[^/@\s]*@`)

// appendRepository records a repository on a project, matching on canonical
// Identity. When the identity already exists but was stored without a clone URL
// (a machine that only knew the canonical key), a newly observed credential-free
// URL fills the gap: real clone URLs are learned lazily as machines with actual
// remotes touch the project.
func appendRepository(repos []vault.Repository, repo vault.Repository) []vault.Repository {
	for i, existing := range repos {
		if existing.Identity == repo.Identity {
			if existing.CloneURL == "" && repo.CloneURL != "" {
				repos[i].CloneURL = repo.CloneURL
			}
			return repos
		}
	}
	return append(repos, repo)
}

func Pull(cfg vault.LocalConfig) error { return gitops.Pull(cfg.VaultPath) }
func Push(cfg vault.LocalConfig) error {
	return gitops.CommitAndPush(cfg.VaultPath, "gitenv: update encrypted profiles")
}
func PushExisting(cfg vault.LocalConfig) error { return gitops.Push(cfg.VaultPath) }
func InspectSync(cfg vault.LocalConfig) gitops.SyncStatus {
	return gitops.InspectSync(cfg.VaultPath)
}
func HasRemote(cfg vault.LocalConfig) bool {
	return cfg.VaultPath != "" && gitops.HasRemote(cfg.VaultPath, "origin")
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// samePath compares two local paths. Windows and macOS default to
// case-insensitive filesystems, so "C:\Dev\api" and "c:\dev\api" are the same
// directory and must link to the same project.
func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	aa, bb = filepath.Clean(aa), filepath.Clean(bb)
	if caseInsensitiveFS {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}

// caseInsensitiveFS reflects the platform default. It is deliberately not a
// per-volume probe: a wrong "same path" answer only ever merges two links that
// already point at the same directory.
var caseInsensitiveFS = runtime.GOOS == "windows" || runtime.GOOS == "darwin"
