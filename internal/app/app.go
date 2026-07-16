package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

type CurrentProject struct {
	Path       string
	Name       string
	HasEnv     bool
	LinkedName string
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
	if err := gitops.Clone(remoteURL, absolute); err != nil {
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

func ExportIdentity(target string) error {
	identityPath, err := vault.IdentityPath()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(expandHome(target))
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolute); err == nil {
		return fmt.Errorf("refusing to overwrite %s", absolute)
	}
	data, err := os.ReadFile(identityPath)
	if err != nil {
		return err
	}
	return vault.WriteAtomic(absolute, data, 0o600)
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
	identity, err := vault.ParseIdentity(data)
	if err != nil {
		return err
	}
	return vault.SaveIdentity(identity)
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
	}
	return current, nil
}

func AddCurrentProject(cfg *vault.LocalConfig, current CurrentProject, name, profile string) error {
	if !current.HasEnv {
		return errors.New("current directory has no .env file")
	}
	if err := vault.Link(cfg, name, current.Path); err != nil {
		return err
	}
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
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
	if err := vault.SaveLocal(*cfg); err != nil {
		return err
	}
	return vault.Apply(cfg, name, profile, true)
}

func AddRemote(cfg vault.LocalConfig, remoteURL string) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	return gitops.AddRemote(cfg.VaultPath, "origin", remoteURL)
}

func Pull(cfg vault.LocalConfig) error { return gitops.Pull(cfg.VaultPath) }
func Push(cfg vault.LocalConfig) error {
	return gitops.CommitAndPush(cfg.VaultPath, "gitenv: update encrypted profiles")
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

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(aa) == filepath.Clean(bb)
}
