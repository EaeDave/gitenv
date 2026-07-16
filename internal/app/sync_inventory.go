package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/eaedave/gitenv/internal/envdiff"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

type LocalEnvDelta struct {
	Project string
	Profile string
	Diff    envdiff.Diff
}

type SyncInventory struct {
	Committed   vault.VaultDelta
	Uncommitted vault.VaultDelta
	LocalEnvs   []LocalEnvDelta
	Available   bool
	Detail      string
	LocalDetail string
}

func InspectSyncWithInventory(cfg vault.LocalConfig) (gitops.SyncStatus, SyncInventory) {
	status := gitops.InspectSync(cfg.VaultPath)
	inventory, err := inspectSyncInventory(cfg.VaultPath, status)
	if err != nil {
		inventory.Detail = safeInventoryError(err)
	} else {
		inventory.Available = true
	}
	localEnvs, localErr := inspectLocalEnvChanges(cfg)
	inventory.LocalEnvs = localEnvs
	if localErr != nil {
		inventory.LocalDetail = "some local .env change details unavailable"
	}
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

func inspectLocalEnvChanges(cfg vault.LocalConfig) ([]LocalEnvDelta, error) {
	projects := make([]string, 0, len(cfg.Projects))
	for project := range cfg.Projects {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	deltas := make([]LocalEnvDelta, 0)
	var firstErr error
	for _, project := range projects {
		local := cfg.Projects[project]
		if local.ActiveProfile == "" {
			continue
		}
		current, err := os.ReadFile(filepath.Join(local.Path, ".env"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("read local environment for %q: %w", project, err)
			}
			continue
		}
		base, err := vault.ReadProfile(&cfg, project, local.ActiveProfile)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read active profile for %q: %w", project, err)
			}
			continue
		}
		diff := envdiff.Compare(base, current)
		if !diff.Empty() {
			deltas = append(deltas, LocalEnvDelta{Project: project, Profile: local.ActiveProfile, Diff: diff})
		}
	}
	return deltas, firstErr
}

func safeInventoryError(err error) string {
	if err == nil {
		return ""
	}
	return "change details unavailable"
}
