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
