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

func CreateProtectedVault(cfg *vault.LocalConfig, root, password, confirmPassword, remoteURL, deviceName string, mode vault.IdentityStoreMode) error {
	if password == "" || password != confirmPassword {
		return errors.New("master passwords are empty or do not match")
	}
	if err := vault.ValidateMasterPassword(password); err != nil {
		return err
	}
	absolute, err := filepath.Abs(expandHome(root))
	if err != nil {
		return err
	}
	identity, err := vault.GenerateIdentity()
	if err != nil {
		return err
	}
	vault.SetSessionIdentity(identity)
	if err := vault.Init(absolute, identity.Recipient().String()); err != nil {
		return err
	}
	if err := gitops.Init(absolute); err != nil {
		return err
	}
	if err := vault.ProtectVault(absolute, password, deviceName, vault.StoreIdentitySession); err != nil {
		return err
	}
	if err := vault.StoreUnlockedIdentity(identity, mode); err != nil {
		return err
	}
	if strings.TrimSpace(remoteURL) != "" {
		if err := gitops.AddRemote(absolute, "origin", remoteURL); err != nil {
			return err
		}
	}
	cfg.VaultPath = absolute
	if cfg.Projects == nil {
		cfg.Projects = map[string]vault.LocalProject{}
	}
	return vault.SaveLocal(*cfg)
}

func CloneLockedVault(cfg *vault.LocalConfig, remoteURL, root string) error {
	absolute, err := filepath.Abs(expandHome(root))
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(absolute, "gitenv.json")); errors.Is(err, os.ErrNotExist) {
		if err := gitops.Clone(remoteURL, absolute); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := vault.LoadManifest(absolute); err != nil {
		return fmt.Errorf("repository is not a gitenv vault: %w", err)
	}
	cfg.VaultPath = absolute
	if cfg.Projects == nil {
		cfg.Projects = map[string]vault.LocalProject{}
	}
	return vault.SaveLocal(*cfg)
}

func CloneProtectedVault(cfg *vault.LocalConfig, remoteURL, root, password string, mode vault.IdentityStoreMode) error {
	if password == "" {
		return errors.New("master password is required")
	}
	if err := CloneLockedVault(cfg, remoteURL, root); err != nil {
		return err
	}
	return vault.UnlockVault(cfg.VaultPath, password, mode)
}

func MigrateVaultAccess(cfg vault.LocalConfig, password, confirmPassword, deviceName string, mode vault.IdentityStoreMode) error {
	if password == "" || password != confirmPassword {
		return errors.New("master passwords are empty or do not match")
	}
	if err := vault.ValidateMasterPassword(password); err != nil {
		return err
	}
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	return vault.ProtectVault(cfg.VaultPath, password, deviceName, mode)
}
