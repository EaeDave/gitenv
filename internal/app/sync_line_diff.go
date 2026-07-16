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

type LocalEnvLineDelta struct {
	Project string
	Profile string
	Lines   []envdiff.LineChange
}

type SyncLineDiff struct {
	LocalEnvs   []LocalEnvLineDelta
	Committed   []vault.ProfileLineDelta
	Uncommitted []vault.ProfileLineDelta
}

func RevealSyncLineDiff(cfg vault.LocalConfig, status gitops.SyncStatus) (SyncLineDiff, error) {
	localEnvs, err := revealLocalEnvLines(cfg)
	if err != nil {
		return SyncLineDiff{}, err
	}
	headFiles := map[string][]byte{}
	if gitops.HasHead(cfg.VaultPath) {
		headFiles, err = gitops.RevisionFiles(cfg.VaultPath, "HEAD")
		if err != nil {
			return SyncLineDiff{}, fmt.Errorf("read HEAD for plaintext diff: %w", err)
		}
	}
	worktreeFiles, err := gitops.WorktreeFiles(cfg.VaultPath)
	if err != nil {
		return SyncLineDiff{}, fmt.Errorf("read worktree for plaintext diff: %w", err)
	}
	uncommitted, err := vault.CompareVaultSnapshotLines(headFiles, worktreeFiles)
	if err != nil {
		return SyncLineDiff{}, fmt.Errorf("compare uncommitted plaintext: %w", err)
	}
	result := SyncLineDiff{LocalEnvs: localEnvs, Uncommitted: uncommitted}
	if status.State != gitops.SyncLocalAhead && status.State != gitops.SyncRemoteAhead {
		return result, nil
	}
	upstream, err := gitops.UpstreamRevision(cfg.VaultPath)
	if err != nil {
		if status.State == gitops.SyncLocalAhead {
			result.Committed, err = vault.CompareVaultSnapshotLines(map[string][]byte{}, headFiles)
			return result, err
		}
		return SyncLineDiff{}, err
	}
	upstreamFiles, err := gitops.RevisionFiles(cfg.VaultPath, upstream)
	if err != nil {
		return SyncLineDiff{}, fmt.Errorf("read upstream for plaintext diff: %w", err)
	}
	if status.State == gitops.SyncRemoteAhead {
		result.Committed, err = vault.CompareVaultSnapshotLines(headFiles, upstreamFiles)
	} else {
		result.Committed, err = vault.CompareVaultSnapshotLines(upstreamFiles, headFiles)
	}
	if err != nil {
		return SyncLineDiff{}, fmt.Errorf("compare committed plaintext: %w", err)
	}
	return result, nil
}

func revealLocalEnvLines(cfg vault.LocalConfig) ([]LocalEnvLineDelta, error) {
	projects := make([]string, 0, len(cfg.Projects))
	for project := range cfg.Projects {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	deltas := make([]LocalEnvLineDelta, 0)
	for _, project := range projects {
		local := cfg.Projects[project]
		if local.ActiveProfile == "" {
			continue
		}
		current, err := os.ReadFile(filepath.Join(local.Path, ".env"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read local environment for %q: %w", project, err)
		}
		base, err := vault.ReadProfile(&cfg, project, local.ActiveProfile)
		if err != nil {
			return nil, fmt.Errorf("read active profile for %q: %w", project, err)
		}
		if lines := envdiff.CompareLines(base, current); hasChangedLines(lines) {
			deltas = append(deltas, LocalEnvLineDelta{Project: project, Profile: local.ActiveProfile, Lines: lines})
		}
	}
	return deltas, nil
}

func hasChangedLines(lines []envdiff.LineChange) bool {
	for _, line := range lines {
		if line.Kind != envdiff.LineContext {
			return true
		}
	}
	return false
}
