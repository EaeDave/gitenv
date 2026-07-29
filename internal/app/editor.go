package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/eaedave/gitenv/internal/vault"
)

// localEnvPath resolves a linked project's managed env file to an absolute host
// path, honoring a per-project EnvFile instead of a hardcoded ".env".
//
// With no vault configured there is no project metadata to consult, so the
// managed file is the default env file. That is a defined state, not a failure:
// a manifest that exists but cannot be read still surfaces its error, because
// silently defaulting there could write to the wrong file.
func localEnvPath(cfg vault.LocalConfig, project string, local vault.LocalProject) (string, error) {
	if cfg.VaultPath == "" {
		return vault.EnvPath(local, vault.Project{})
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return "", err
	}
	entry, err := vault.ProjectEntry(manifest, project)
	if err != nil {
		return "", err
	}
	return vault.EnvPath(local, entry)
}

// ReadLocalEnv returns the exact bytes of a linked project's local env file, or
// nil when the file does not exist yet.
func ReadLocalEnv(cfg vault.LocalConfig, project string) ([]byte, error) {
	local, ok := cfg.Projects[project]
	if !ok {
		return nil, fmt.Errorf("project %q is not linked on this computer", project)
	}
	path, err := localEnvPath(cfg, project, local)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// WriteLocalEnv writes bytes verbatim to a linked project's local env file.
func WriteLocalEnv(cfg vault.LocalConfig, project string, data []byte) error {
	local, ok := cfg.Projects[project]
	if !ok {
		return fmt.Errorf("project %q is not linked on this computer", project)
	}
	path, err := localEnvPath(cfg, project, local)
	if err != nil {
		return err
	}
	return vault.WriteAtomic(path, data, 0o600)
}

// ReadActiveProfileEnv decrypts the bytes of a project's active profile so the
// editor can show a live diff between the local .env and what is captured in
// the vault. It reports available=false (without error) when the project has no
// active profile yet, so a brand-new .env simply has no baseline to compare.
func ReadActiveProfileEnv(cfg vault.LocalConfig, project string) (data []byte, profile string, available bool, err error) {
	local, ok := cfg.Projects[project]
	if !ok || local.ActiveProfile == "" {
		return nil, "", false, nil
	}
	data, err = vault.ReadProfile(&cfg, project, local.ActiveProfile)
	if err != nil {
		return nil, local.ActiveProfile, false, err
	}
	return data, local.ActiveProfile, true, nil
}
