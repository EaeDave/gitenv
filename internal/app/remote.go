package app

import (
	"errors"
	"fmt"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// VaultRemoteURL returns the URL of the vault's origin remote.
func VaultRemoteURL(cfg vault.LocalConfig) (string, error) {
	if cfg.VaultPath == "" {
		return "", errors.New("no vault configured")
	}
	url, err := gitops.RemoteURL(cfg.VaultPath, "origin")
	if err != nil {
		return "", fmt.Errorf("read vault remote: %w", err)
	}
	return url, nil
}

func VaultRemoteDisplayURL(cfg vault.LocalConfig) string {
	value, err := VaultRemoteURL(cfg)
	if err != nil {
		return ""
	}
	return gitops.RedactURL(value)
}

// ConfigureVaultRemote sets the vault's origin remote URL. It adds the remote
// if none exists, or updates the existing URL in place.
func ConfigureVaultRemote(cfg vault.LocalConfig, remoteURL string) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	if gitops.HasRemote(cfg.VaultPath, "origin") {
		return gitops.SetRemoteURL(cfg.VaultPath, "origin", remoteURL)
	}
	return gitops.AddRemote(cfg.VaultPath, "origin", remoteURL)
}

// RemoveVaultRemote deletes the vault's origin remote.
func RemoveVaultRemote(cfg vault.LocalConfig) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	return gitops.RemoveRemote(cfg.VaultPath, "origin")
}

// TestVaultRemote checks whether the vault's origin remote is reachable.
// It connects to the remote without fetching or mutating any local state.
func TestVaultRemote(cfg vault.LocalConfig) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	return gitops.PingRemote(cfg.VaultPath, "origin")
}
