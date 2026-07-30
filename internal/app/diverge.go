package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// backupPrefix names the branch namespace every recoverable pre-resolution
// snapshot lives under, so a user can always find where their old vault went.
const backupPrefix = "gitenv-backup"

// DivergenceChoice records, for a profile changed on both sides, which version
// the user keeps.
type DivergenceChoice string

const (
	DivergenceKeepMine   DivergenceChoice = "mine"
	DivergenceTakeRemote DivergenceChoice = "remote"
)

// DivergedProfile is one profile touched on at least one side since the split.
type DivergedProfile struct {
	Project, Profile string
	ChangedLocally   bool
	ChangedRemotely  bool
}

// key is the map key used to address a profile in a choices map.
func (p DivergedProfile) key() string { return p.Project + "/" + p.Profile }

type DivergenceReport struct {
	LocalCommits, RemoteCommits int
	Profiles                    []DivergedProfile // sorted by project then profile
}

// Conflicted reports the profiles changed on BOTH sides, which are the only
// ones the user must actually decide about.
func (r DivergenceReport) Conflicted() []DivergedProfile {
	conflicted := make([]DivergedProfile, 0, len(r.Profiles))
	for _, profile := range r.Profiles {
		if profile.ChangedLocally && profile.ChangedRemotely {
			conflicted = append(conflicted, profile)
		}
	}
	return conflicted
}

// InspectDivergence describes a diverged vault without mutating anything. It
// diffs each side against their common ancestor so it can attribute every
// touched profile to the local side, the remote side, or both.
func InspectDivergence(cfg vault.LocalConfig) (DivergenceReport, error) {
	root := cfg.VaultPath
	if root == "" {
		return DivergenceReport{}, errors.New("no vault configured")
	}
	if !gitops.HasHead(root) {
		return DivergenceReport{}, errors.New("vault has no history yet")
	}
	upstream, err := gitops.UpstreamRevision(root)
	if err != nil {
		return DivergenceReport{}, fmt.Errorf("resolve remote vault revision: %w", err)
	}
	base, err := gitops.MergeBase(root, "HEAD", upstream)
	if err != nil {
		return DivergenceReport{}, fmt.Errorf("find common vault ancestor: %w", err)
	}
	local, remote, err := gitops.AheadBehind(root)
	if err != nil {
		return DivergenceReport{}, fmt.Errorf("count diverged vault changes: %w", err)
	}
	baseFiles, err := gitops.RevisionFiles(root, base)
	if err != nil {
		return DivergenceReport{}, err
	}
	headFiles, err := gitops.RevisionFiles(root, "HEAD")
	if err != nil {
		return DivergenceReport{}, err
	}
	upstreamFiles, err := gitops.RevisionFiles(root, upstream)
	if err != nil {
		return DivergenceReport{}, err
	}
	localDelta, err := vault.CompareVaultSnapshots(baseFiles, headFiles)
	if err != nil {
		return DivergenceReport{}, fmt.Errorf("compare local vault changes: %w", err)
	}
	remoteDelta, err := vault.CompareVaultSnapshots(baseFiles, upstreamFiles)
	if err != nil {
		return DivergenceReport{}, fmt.Errorf("compare remote vault changes: %w", err)
	}
	return DivergenceReport{
		LocalCommits:  local,
		RemoteCommits: remote,
		Profiles:      mergeDivergedProfiles(localDelta.Profiles, remoteDelta.Profiles),
	}, nil
}

// mergeDivergedProfiles folds the two per-side profile diffs into one sorted
// list, marking which side (or both) touched each profile.
func mergeDivergedProfiles(local, remote []vault.ProfileDelta) []DivergedProfile {
	index := map[string]*DivergedProfile{}
	touch := func(delta vault.ProfileDelta, isLocal bool) {
		profile, ok := index[delta.Project+"/"+delta.Profile]
		if !ok {
			profile = &DivergedProfile{Project: delta.Project, Profile: delta.Profile}
			index[delta.Project+"/"+delta.Profile] = profile
		}
		if isLocal {
			profile.ChangedLocally = true
		} else {
			profile.ChangedRemotely = true
		}
	}
	for _, delta := range local {
		touch(delta, true)
	}
	for _, delta := range remote {
		touch(delta, false)
	}
	profiles := make([]DivergedProfile, 0, len(index))
	for _, profile := range index {
		profiles = append(profiles, *profile)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Project != profiles[j].Project {
			return profiles[i].Project < profiles[j].Project
		}
		return profiles[i].Profile < profiles[j].Profile
	})
	return profiles
}

// keptProfile is one profile whose local ciphertext is carried across the reset
// and written back on top of the remote.
type keptProfile struct {
	project    string
	profile    string
	ciphertext []byte
	checksum   string
	updatedAt  time.Time
	// meta seeds project metadata when the project is absent from the remote
	// (added only locally), so its env-file and line-ending settings survive.
	meta vault.Project
}

// ResolveDivergence rebuilds the vault on top of the remote, keeping the local
// version of every profile the user chose to keep. Returns the backup ref.
//
// Local-only profile changes are kept automatically; remote-only changes are
// accepted automatically. Only profiles changed on both sides consult choices,
// where a missing entry defaults to taking the remote — the safer choice, since
// the local version is still reachable from the backup ref.
func ResolveDivergence(cfg *vault.LocalConfig, choices map[string]DivergenceChoice) (string, error) {
	root := cfg.VaultPath
	if root == "" {
		return "", errors.New("no vault configured")
	}
	if err := requireCleanWorktree(root); err != nil {
		return "", err
	}
	localManifest, err := vault.LoadManifest(root)
	if err != nil {
		return "", err
	}
	report, err := InspectDivergence(*cfg)
	if err != nil {
		return "", err
	}
	upstream, err := gitops.UpstreamRevision(root)
	if err != nil {
		return "", fmt.Errorf("resolve remote vault revision: %w", err)
	}

	// 1. A recoverable ref before anything destructive happens.
	backup, err := gitops.BackupRef(root, backupPrefix)
	if err != nil {
		return "", fmt.Errorf("create recovery point: %w", err)
	}

	// 2. Capture the local blobs to keep, from the current HEAD, into memory.
	captured, err := captureKeptProfiles(root, localManifest, report, choices)
	if err != nil {
		return backup, err
	}

	// 3. Discard the local commits by rebuilding on the remote.
	if err := gitops.ResetHard(root, upstream); err != nil {
		return backup, err
	}

	// 4. Write the kept ciphertexts back and keep their metadata consistent.
	manifest, err := vault.LoadManifest(root)
	if err != nil {
		return backup, err
	}
	if err := restoreKeptProfiles(root, &manifest, captured); err != nil {
		return backup, err
	}
	if err := vault.SaveManifest(root, manifest); err != nil {
		return backup, fmt.Errorf("write resolved vault metadata: %w", err)
	}

	// 5. Commit the resolution.
	message := fmt.Sprintf("resolve diverged vault, keeping %d local %s", len(captured), pluralProfiles(len(captured)))
	if err := gitops.CommitAll(root, message); err != nil {
		return backup, err
	}
	return backup, nil
}

// DiscardLocalVaultChanges throws away this computer's unpublished vault commits
// and takes the remote as-is. Returns the backup ref.
func DiscardLocalVaultChanges(cfg *vault.LocalConfig) (string, error) {
	root := cfg.VaultPath
	if root == "" {
		return "", errors.New("no vault configured")
	}
	if err := requireCleanWorktree(root); err != nil {
		return "", err
	}
	upstream, err := gitops.UpstreamRevision(root)
	if err != nil {
		return "", fmt.Errorf("resolve remote vault revision: %w", err)
	}
	backup, err := gitops.BackupRef(root, backupPrefix)
	if err != nil {
		return "", fmt.Errorf("create recovery point: %w", err)
	}
	if err := gitops.ResetHard(root, upstream); err != nil {
		return backup, err
	}
	return backup, nil
}

// requireCleanWorktree refuses a resolution while the vault has uncommitted
// changes: the reset would destroy them and they are not in the backup ref.
func requireCleanWorktree(root string) error {
	status, err := gitops.Status(root)
	if err != nil {
		return fmt.Errorf("check vault worktree: %w", err)
	}
	if status != "" {
		return errors.New("the vault has uncaptured changes; publish or discard them before resolving")
	}
	return nil
}

// captureKeptProfiles reads, from the current HEAD, the ciphertext and metadata
// of every profile whose local version must survive the reset.
func captureKeptProfiles(root string, manifest vault.Manifest, report DivergenceReport, choices map[string]DivergenceChoice) ([]keptProfile, error) {
	captured := make([]keptProfile, 0, len(report.Profiles))
	for _, profile := range report.Profiles {
		if !keepLocalVersion(profile, choices) {
			continue
		}
		project, ok := manifest.Projects[profile.Project]
		if !ok {
			continue
		}
		stored, ok := project.Profiles[profile.Profile]
		if !ok {
			continue // removed locally: nothing to carry across
		}
		path, ok := vault.ProfilePath(root, manifest, profile.Project, profile.Profile)
		if !ok {
			continue
		}
		ciphertext, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read local profile %s: %w", profile.key(), err)
		}
		captured = append(captured, keptProfile{
			project:    profile.Project,
			profile:    profile.Profile,
			ciphertext: ciphertext,
			checksum:   stored.Checksum,
			updatedAt:  stored.UpdatedAt,
			meta:       project,
		})
	}
	return captured, nil
}

// keepLocalVersion decides whether a profile's local version is carried across
// the reset: local-only changes always are, both-changed only when the user
// chose to keep theirs, and remote-only changes never are.
func keepLocalVersion(profile DivergedProfile, choices map[string]DivergenceChoice) bool {
	switch {
	case profile.ChangedLocally && profile.ChangedRemotely:
		return choices[profile.key()] == DivergenceKeepMine
	case profile.ChangedLocally:
		return true
	default:
		return false
	}
}

// restoreKeptProfiles writes each captured ciphertext into its v3 path in the
// reset vault and updates the manifest entry so metadata and ciphertext agree.
func restoreKeptProfiles(root string, manifest *vault.Manifest, captured []keptProfile) error {
	for _, kept := range captured {
		if _, ok := manifest.Projects[kept.project]; !ok {
			// Project added only locally: seed its settings from the local
			// metadata, but drop the id and profiles so fresh ones are assigned.
			seed := kept.meta
			seed.ID = ""
			seed.Profiles = map[string]vault.Profile{}
			manifest.Projects[kept.project] = seed
		}
		if _, err := manifest.EnsureProfileID(kept.project, kept.profile); err != nil {
			return fmt.Errorf("assign id for kept profile %s/%s: %w", kept.project, kept.profile, err)
		}
		path, ok := vault.ProfilePath(root, *manifest, kept.project, kept.profile)
		if !ok {
			return fmt.Errorf("resolve path for kept profile %s/%s", kept.project, kept.profile)
		}
		if err := vault.WriteAtomic(path, kept.ciphertext, 0o600); err != nil {
			return fmt.Errorf("write kept profile %s/%s: %w", kept.project, kept.profile, err)
		}
		entry := manifest.Projects[kept.project]
		stored := entry.Profiles[kept.profile]
		stored.Checksum = kept.checksum
		stored.UpdatedAt = kept.updatedAt
		entry.Profiles[kept.profile] = stored
		manifest.Projects[kept.project] = entry
	}
	return nil
}

func pluralProfiles(count int) string {
	if count == 1 {
		return "environment"
	}
	return "environments"
}
