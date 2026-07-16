package app

import (
	"fmt"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func PublishLocalEnv(cfg *vault.LocalConfig, project, profile string) error {
	status := gitops.InspectSync(cfg.VaultPath)
	if status.State != gitops.SyncSynced || status.Dirty {
		return fmt.Errorf("vault must be synchronized and clean before publishing one environment")
	}
	if err := vault.Capture(cfg, project, profile); err != nil {
		return fmt.Errorf("capture %s/%s: %w", project, profile, err)
	}
	if err := Push(*cfg); err != nil {
		return fmt.Errorf("publish %s/%s: %w", project, profile, err)
	}
	return nil
}

func DiscardLocalEnv(cfg *vault.LocalConfig, project, profile string) error {
	if err := vault.Apply(cfg, project, profile, true); err != nil {
		return fmt.Errorf("restore %s/%s: %w", project, profile, err)
	}
	return nil
}
