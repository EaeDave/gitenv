package app

import (
	"fmt"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

type SyncInventory struct {
	Committed   vault.VaultDelta
	Uncommitted vault.VaultDelta
	Available   bool
	Detail      string
}

func InspectSyncWithInventory(cfg vault.LocalConfig) (gitops.SyncStatus, SyncInventory) {
	status := gitops.InspectSync(cfg.VaultPath)
	inventory, err := inspectSyncInventory(cfg.VaultPath, status)
	if err != nil {
		inventory.Detail = safeInventoryError(err)
		return status, inventory
	}
	inventory.Available = true
	return status, inventory
}

func inspectSyncInventory(root string, status gitops.SyncStatus) (SyncInventory, error) {
	headFiles := map[string][]byte{}
	if gitops.HasHead(root) {
		var err error
		headFiles, err = gitops.RevisionFiles(root, "HEAD")
		if err != nil {
			return SyncInventory{}, err
		}
	}
	worktreeFiles, err := gitops.WorktreeFiles(root)
	if err != nil {
		return SyncInventory{}, err
	}
	uncommitted, err := vault.CompareVaultSnapshots(headFiles, worktreeFiles)
	if err != nil {
		return SyncInventory{}, fmt.Errorf("compare uncommitted vault changes: %w", err)
	}
	inventory := SyncInventory{Uncommitted: uncommitted}
	if status.State != gitops.SyncLocalAhead && status.State != gitops.SyncRemoteAhead {
		return inventory, nil
	}
	upstream, err := gitops.UpstreamRevision(root)
	if err != nil {
		if status.State == gitops.SyncLocalAhead {
			inventory.Committed, err = vault.CompareVaultSnapshots(map[string][]byte{}, headFiles)
			return inventory, err
		}
		return SyncInventory{}, err
	}
	upstreamFiles, err := gitops.RevisionFiles(root, upstream)
	if err != nil {
		return SyncInventory{}, err
	}
	if status.State == gitops.SyncRemoteAhead {
		inventory.Committed, err = vault.CompareVaultSnapshots(headFiles, upstreamFiles)
	} else {
		inventory.Committed, err = vault.CompareVaultSnapshots(upstreamFiles, headFiles)
	}
	if err != nil {
		return SyncInventory{}, fmt.Errorf("compare committed vault changes: %w", err)
	}
	return inventory, nil
}

func safeInventoryError(err error) string {
	if err == nil {
		return ""
	}
	return "change details unavailable"
}
