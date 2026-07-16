package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"filippo.io/age"

	"github.com/eaedave/gitenv/internal/envdiff"
)

type ProfileDeltaKind string

const (
	ProfileAdded   ProfileDeltaKind = "added"
	ProfileRemoved ProfileDeltaKind = "removed"
	ProfileChanged ProfileDeltaKind = "changed"
)

type ProfileDelta struct {
	Project string
	Profile string
	Kind    ProfileDeltaKind
	Diff    envdiff.Diff
}

type VaultDelta struct {
	Profiles          []ProfileDelta
	MetadataChanged   bool
	OtherFilesChanged int
}

func (d VaultDelta) Empty() bool {
	return len(d.Profiles) == 0 && !d.MetadataChanged && d.OtherFilesChanged == 0
}

type ProfileLineDelta struct {
	Project string
	Profile string
	Kind    ProfileDeltaKind
	Lines   []envdiff.LineChange
}

// CompareVaultSnapshots returns a value-free semantic diff. Ciphertexts are
// decrypted only in memory, and only when profile checksums differ.
func CompareVaultSnapshots(baseFiles, currentFiles map[string][]byte) (VaultDelta, error) {
	baseManifest, err := manifestFromSnapshot(baseFiles)
	if err != nil {
		return VaultDelta{}, fmt.Errorf("read base vault snapshot: %w", err)
	}
	currentManifest, err := manifestFromSnapshot(currentFiles)
	if err != nil {
		return VaultDelta{}, fmt.Errorf("read current vault snapshot: %w", err)
	}
	delta := VaultDelta{
		MetadataChanged:   metadataChanged(baseManifest, currentManifest),
		OtherFilesChanged: otherFilesChanged(baseFiles, currentFiles),
	}
	identity, identityErr := LoadIdentity()
	for _, ref := range profileUnion(baseManifest, currentManifest) {
		baseProfile, inBase := profileAt(baseManifest, ref)
		currentProfile, inCurrent := profileAt(currentManifest, ref)
		switch {
		case !inBase:
			delta.Profiles = append(delta.Profiles, ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileAdded})
		case !inCurrent:
			delta.Profiles = append(delta.Profiles, ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileRemoved})
		case profileSnapshotChanged(baseFiles, currentFiles, ref, baseProfile, currentProfile):
			profileDelta := ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileChanged}
			if baseProfile.Checksum != currentProfile.Checksum {
				if identityErr != nil {
					return VaultDelta{}, fmt.Errorf("load identity for vault diff: %w", identityErr)
				}
				basePlaintext, err := decryptSnapshotProfile(baseFiles, ref, baseProfile.Checksum, identity)
				if err != nil {
					return VaultDelta{}, fmt.Errorf("read base profile %s/%s: %w", ref.project, ref.profile, err)
				}
				currentPlaintext, err := decryptSnapshotProfile(currentFiles, ref, currentProfile.Checksum, identity)
				if err != nil {
					return VaultDelta{}, fmt.Errorf("read current profile %s/%s: %w", ref.project, ref.profile, err)
				}
				profileDelta.Diff = envdiff.Compare(basePlaintext, currentPlaintext)
			}
			delta.Profiles = append(delta.Profiles, profileDelta)
		}
	}
	return delta, nil
}

// CompareVaultSnapshotLines decrypts changed profiles in memory for an
// explicitly requested plaintext view. Callers must discard the result when
// the view is hidden or closed.
func CompareVaultSnapshotLines(baseFiles, currentFiles map[string][]byte, scope string) ([]ProfileLineDelta, error) {
	baseManifest, err := manifestFromSnapshot(baseFiles)
	if err != nil {
		return nil, fmt.Errorf("read base vault snapshot: %w", err)
	}
	currentManifest, err := manifestFromSnapshot(currentFiles)
	if err != nil {
		return nil, fmt.Errorf("read current vault snapshot: %w", err)
	}
	identity, identityErr := LoadIdentity()
	deltas := make([]ProfileLineDelta, 0)
	for _, ref := range profileUnion(baseManifest, currentManifest) {
		if scope != "" && ref.project != scope {
			continue
		}
		baseProfile, inBase := profileAt(baseManifest, ref)
		currentProfile, inCurrent := profileAt(currentManifest, ref)
		if inBase && inCurrent && baseProfile.Checksum == currentProfile.Checksum {
			continue
		}
		if identityErr != nil {
			return nil, fmt.Errorf("load identity for plaintext vault diff: %w", identityErr)
		}
		var basePlaintext, currentPlaintext []byte
		kind := ProfileChanged
		if inBase {
			basePlaintext, err = decryptSnapshotProfile(baseFiles, ref, baseProfile.Checksum, identity)
			if err != nil {
				return nil, fmt.Errorf("read base profile %s/%s: %w", ref.project, ref.profile, err)
			}
		} else {
			kind = ProfileAdded
		}
		if inCurrent {
			currentPlaintext, err = decryptSnapshotProfile(currentFiles, ref, currentProfile.Checksum, identity)
			if err != nil {
				return nil, fmt.Errorf("read current profile %s/%s: %w", ref.project, ref.profile, err)
			}
		} else {
			kind = ProfileRemoved
		}
		deltas = append(deltas, ProfileLineDelta{
			Project: ref.project,
			Profile: ref.profile,
			Kind:    kind,
			Lines:   envdiff.CompareLines(basePlaintext, currentPlaintext),
		})
	}
	return deltas, nil
}

func profileSnapshotChanged(baseFiles, currentFiles map[string][]byte, ref profileRef, base, current Profile) bool {
	if base.Checksum != current.Checksum || !base.UpdatedAt.Equal(current.UpdatedAt) {
		return true
	}
	name := path.Join("projects", ref.project, ref.profile+".env.age")
	return !bytes.Equal(baseFiles[name], currentFiles[name])
}

type profileRef struct {
	project string
	profile string
}

func manifestFromSnapshot(files map[string][]byte) (Manifest, error) {
	if len(files) == 0 {
		return Manifest{Version: ManifestVersion, Projects: map[string]Project{}}, nil
	}
	data, ok := files[manifestName]
	if !ok {
		return Manifest{}, errors.New("gitenv.json is missing")
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid manifest: %w", err)
	}
	if manifest.Version < 1 || manifest.Version > ManifestVersion {
		return Manifest{}, fmt.Errorf("unsupported vault version %d", manifest.Version)
	}
	if manifest.Projects == nil {
		manifest.Projects = map[string]Project{}
	}
	return manifest, nil
}

func profileUnion(base, current Manifest) []profileRef {
	refs := make(map[profileRef]struct{})
	for project, entry := range base.Projects {
		for profile := range entry.Profiles {
			refs[profileRef{project: project, profile: profile}] = struct{}{}
		}
	}
	for project, entry := range current.Projects {
		for profile := range entry.Profiles {
			refs[profileRef{project: project, profile: profile}] = struct{}{}
		}
	}
	ordered := make([]profileRef, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].project == ordered[j].project {
			return ordered[i].profile < ordered[j].profile
		}
		return ordered[i].project < ordered[j].project
	})
	return ordered
}

func profileAt(manifest Manifest, ref profileRef) (Profile, bool) {
	project, ok := manifest.Projects[ref.project]
	if !ok {
		return Profile{}, false
	}
	profile, ok := project.Profiles[ref.profile]
	return profile, ok
}

func decryptSnapshotProfile(files map[string][]byte, ref profileRef, checksum string, identity age.Identity) ([]byte, error) {
	ciphertext, ok := files[path.Join("projects", ref.project, ref.profile+".env.age")]
	if !ok {
		return nil, errors.New("encrypted profile is missing")
	}
	plaintext, err := Decrypt(ciphertext, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt profile: %w", err)
	}
	if Checksum(plaintext) != checksum {
		return nil, errors.New("decrypted profile checksum mismatch")
	}
	return plaintext, nil
}

func metadataChanged(base, current Manifest) bool {
	metadata := func(manifest Manifest) Manifest {
		projectMetadata := make(map[string]Project, len(manifest.Projects))
		for name, project := range manifest.Projects {
			projectMetadata[name] = Project{Repositories: append([]string(nil), project.Repositories...)}
		}
		manifest.Projects = projectMetadata
		return manifest
	}
	baseJSON, _ := json.Marshal(metadata(base))
	currentJSON, _ := json.Marshal(metadata(current))
	return !bytes.Equal(baseJSON, currentJSON)
}

func otherFilesChanged(base, current map[string][]byte) int {
	paths := make(map[string]struct{}, len(base)+len(current))
	for name := range base {
		paths[name] = struct{}{}
	}
	for name := range current {
		paths[name] = struct{}{}
	}
	changed := 0
	for name := range paths {
		if name == manifestName || strings.HasPrefix(name, "projects/") {
			continue
		}
		if !bytes.Equal(base[name], current[name]) {
			changed++
		}
	}
	return changed
}
