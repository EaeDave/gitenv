package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eaedave/gitenv/internal/vault"
)

// ReadLocalEnv returns the exact bytes of a linked project's local .env, or nil
// when the file does not exist yet.
func ReadLocalEnv(cfg vault.LocalConfig, project string) ([]byte, error) {
	local, ok := cfg.Projects[project]
	if !ok {
		return nil, fmt.Errorf("project %q is not linked on this computer", project)
	}
	data, err := os.ReadFile(filepath.Join(local.Path, ".env"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// WriteLocalEnv writes bytes verbatim to a linked project's local .env.
func WriteLocalEnv(cfg vault.LocalConfig, project string, data []byte) error {
	local, ok := cfg.Projects[project]
	if !ok {
		return fmt.Errorf("project %q is not linked on this computer", project)
	}
	return vault.WriteAtomic(filepath.Join(local.Path, ".env"), data, 0o600)
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
