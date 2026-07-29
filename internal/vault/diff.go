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
// decrypted only in memory, and only when profile checksums differ. Project
// enumeration now needs the vault identity because metadata lives in encrypted
// meta.age files, so the identity is loaded once up front and threaded through.
func CompareVaultSnapshots(baseFiles, currentFiles map[string][]byte) (VaultDelta, error) {
	identity, identityErr := LoadIdentity()
	if identityErr != nil && (snapshotHasMetadata(baseFiles) || snapshotHasMetadata(currentFiles)) {
		return VaultDelta{}, fmt.Errorf("load identity for vault diff: %w", identityErr)
	}
	baseManifest, err := manifestFromSnapshot(baseFiles, identity)
	if err != nil {
		return VaultDelta{}, fmt.Errorf("read base vault snapshot: %w", err)
	}
	currentManifest, err := manifestFromSnapshot(currentFiles, identity)
	if err != nil {
		return VaultDelta{}, fmt.Errorf("read current vault snapshot: %w", err)
	}
	delta := VaultDelta{
		MetadataChanged:   metadataChanged(baseManifest, currentManifest),
		OtherFilesChanged: otherFilesChanged(baseFiles, currentFiles),
	}
	for _, ref := range profileUnion(baseManifest, currentManifest) {
		baseProfile, inBase := profileAt(baseManifest, ref)
		currentProfile, inCurrent := profileAt(currentManifest, ref)
		switch {
		case !inBase:
			delta.Profiles = append(delta.Profiles, ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileAdded})
		case !inCurrent:
			delta.Profiles = append(delta.Profiles, ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileRemoved})
		case profileSnapshotChanged(baseManifest, currentManifest, baseFiles, currentFiles, ref, baseProfile, currentProfile):
			profileDelta := ProfileDelta{Project: ref.project, Profile: ref.profile, Kind: ProfileChanged}
			if baseProfile.Checksum != currentProfile.Checksum {
				basePlaintext, err := decryptSnapshotProfile(baseManifest, baseFiles, ref, baseProfile.Checksum, identity)
				if err != nil {
					return VaultDelta{}, fmt.Errorf("read base profile %s/%s: %w", ref.project, ref.profile, err)
				}
				currentPlaintext, err := decryptSnapshotProfile(currentManifest, currentFiles, ref, currentProfile.Checksum, identity)
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
	identity, identityErr := LoadIdentity()
	if identityErr != nil && (snapshotHasMetadata(baseFiles) || snapshotHasMetadata(currentFiles)) {
		return nil, fmt.Errorf("load identity for plaintext vault diff: %w", identityErr)
	}
	baseManifest, err := manifestFromSnapshot(baseFiles, identity)
	if err != nil {
		return nil, fmt.Errorf("read base vault snapshot: %w", err)
	}
	currentManifest, err := manifestFromSnapshot(currentFiles, identity)
	if err != nil {
		return nil, fmt.Errorf("read current vault snapshot: %w", err)
	}
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
		var basePlaintext, currentPlaintext []byte
		kind := ProfileChanged
		if inBase {
			basePlaintext, err = decryptSnapshotProfile(baseManifest, baseFiles, ref, baseProfile.Checksum, identity)
			if err != nil {
				return nil, fmt.Errorf("read base profile %s/%s: %w", ref.project, ref.profile, err)
			}
		} else {
			kind = ProfileAdded
		}
		if inCurrent {
			currentPlaintext, err = decryptSnapshotProfile(currentManifest, currentFiles, ref, currentProfile.Checksum, identity)
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

func profileSnapshotChanged(baseManifest, currentManifest Manifest, baseFiles, currentFiles map[string][]byte, ref profileRef, base, current Profile) bool {
	if base.Checksum != current.Checksum || !base.UpdatedAt.Equal(current.UpdatedAt) {
		return true
	}
	basePath, _ := snapshotProfileRelPath(baseManifest, ref)
	currentPath, _ := snapshotProfileRelPath(currentManifest, ref)
	return !bytes.Equal(baseFiles[basePath], currentFiles[currentPath])
}

type profileRef struct {
	project string
	profile string
}

// snapshotProfileRelPath resolves a profile's ciphertext key using the ids
// recorded in the given snapshot's metadata. base and current may assign
// different ids to the same project name, so each side resolves with its own
// manifest.
func snapshotProfileRelPath(manifest Manifest, ref profileRef) (string, bool) {
	entry, ok := manifest.Projects[ref.project]
	if !ok || entry.ID == "" {
		return "", false
	}
	profile, ok := entry.Profiles[ref.profile]
	if !ok || profile.ID == "" {
		return "", false
	}
	return profileRelPath(entry.ID, profile.ID), true
}

// metadataSnapshotID returns the project id encoded in a projects/<id>/meta.age
// snapshot key, reporting false for any other path.
func metadataSnapshotID(relPath string) (string, bool) {
	if path.Base(relPath) != metadataName {
		return "", false
	}
	dir := path.Dir(relPath)
	if path.Dir(dir) != projectsDir {
		return "", false
	}
	id := path.Base(dir)
	if !isID(id) {
		return "", false
	}
	return id, true
}

func snapshotHasMetadata(files map[string][]byte) bool {
	for relPath := range files {
		if _, ok := metadataSnapshotID(relPath); ok {
			return true
		}
	}
	return false
}

// manifestFromSnapshot parses gitenv.json from a snapshot and decrypts every
// projects/<id>/meta.age present into Projects, keyed by project name.
func manifestFromSnapshot(files map[string][]byte, identity age.Identity) (Manifest, error) {
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
	manifest.Projects = map[string]Project{}
	ids := make([]string, 0, len(files))
	for relPath := range files {
		if id, ok := metadataSnapshotID(relPath); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		plaintext, err := Decrypt(files[metadataRelPath(id)], identity)
		if err != nil {
			return Manifest{}, fmt.Errorf("decrypt project metadata %s: %w", id, err)
		}
		entry, err := unmarshalProject(plaintext, id)
		if err != nil {
			return Manifest{}, fmt.Errorf("parse project metadata %s: %w", id, err)
		}
		if existing, ok := manifest.Projects[entry.Name]; ok {
			return Manifest{}, fmt.Errorf("duplicate project name %q in metadata %s and %s", entry.Name, existing.ID, id)
		}
		manifest.Projects[entry.Name] = entry
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

func decryptSnapshotProfile(manifest Manifest, files map[string][]byte, ref profileRef, checksum string, identity age.Identity) ([]byte, error) {
	relPath, ok := snapshotProfileRelPath(manifest, ref)
	if !ok {
		return nil, errors.New("encrypted profile is missing")
	}
	ciphertext, ok := files[relPath]
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

// projectFingerprint captures the per-project metadata that a diff attributes to
// metadata rather than to a profile. Profile checksums and timestamps are
// deliberately excluded because profile lifecycle is reported as ProfileDelta.
type projectFingerprint struct {
	Name         string           `json:"name"`
	Repositories []Repository     `json:"repositories"`
	EnvFile      string           `json:"env_file"`
	LineEndings  LineEndingPolicy `json:"line_endings"`
}

// metadataChanged reports whether anything the diff should treat as a metadata
// change differs between the two snapshots: the plaintext bootstrap fields plus
// each project's name, repositories, env file and line-ending policy. It is
// deterministic and never exposes a value.
func metadataChanged(base, current Manifest) bool {
	return !bytes.Equal(metadataFingerprint(base), metadataFingerprint(current))
}

func metadataFingerprint(manifest Manifest) []byte {
	// json.Marshal drops Projects/Sealed/baseline (json:"-"), leaving only the
	// plaintext bootstrap fields (version, recipients, devices, wrapped identity,
	// enrollment requests).
	plaintext, _ := json.Marshal(manifest)
	names := make([]string, 0, len(manifest.Projects))
	for name := range manifest.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	prints := make([]projectFingerprint, 0, len(names))
	for _, name := range names {
		p := manifest.Projects[name]
		prints = append(prints, projectFingerprint{
			Name:         p.Name,
			Repositories: p.Repositories,
			EnvFile:      p.EnvFile,
			LineEndings:  p.LineEndings,
		})
	}
	projectsJSON, _ := json.Marshal(prints)
	return append(plaintext, projectsJSON...)
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
		if name == manifestName || strings.HasPrefix(name, projectsDir+"/") {
			continue
		}
		if !bytes.Equal(base[name], current[name]) {
			changed++
		}
	}
	return changed
}
