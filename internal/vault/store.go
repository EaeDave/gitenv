package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"filippo.io/age"
)

const (
	manifestName = "gitenv.json"
	identityName = "identity.txt"
	localName    = "config.json"
)

func ConfigDir() (string, error) {
	if root := os.Getenv("GITENV_CONFIG_DIR"); root != "" {
		return root, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "gitenv"), nil
}

func localConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, localName), nil
}

func IdentityPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, identityName), nil
}

func LoadLocal() (LocalConfig, error) {
	path, err := localConfigPath()
	if err != nil {
		return LocalConfig{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LocalConfig{Projects: map[string]LocalProject{}}, nil
	}
	if err != nil {
		return LocalConfig{}, err
	}
	var cfg LocalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return LocalConfig{}, fmt.Errorf("invalid local config: %w", err)
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]LocalProject{}
	}
	return cfg, nil
}

func ResetMissingVault(cfg *LocalConfig) (bool, error) {
	if cfg.VaultPath == "" {
		return false, nil
	}
	_, err := os.Stat(filepath.Join(cfg.VaultPath, manifestName))
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	cfg.VaultPath = ""
	cfg.Projects = map[string]LocalProject{}
	return true, nil
}

func SaveLocal(cfg LocalConfig) error {
	path, err := localConfigPath()
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, cfg, 0o600)
}

// LoadManifest reads gitenv.json and, for a v3 vault, decrypts every
// projects/<id>/meta.age into Projects. A v3 vault whose metadata cannot be read
// with any available identity is returned Sealed (no error) so the TUI can still
// render an unlock screen; SaveManifest refuses to write a sealed manifest. An
// older vault is returned with its version preserved and no projects: callers
// upgrade explicitly with UpgradeManifest and loading must never mutate a vault.
func LoadManifest(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid vault manifest: %w", err)
	}
	if manifest.Version < 1 || manifest.Version > ManifestVersion {
		return Manifest{}, fmt.Errorf("unsupported vault version %d", manifest.Version)
	}
	manifest.Projects = map[string]Project{}
	manifest.baseline = map[string][]byte{}
	if manifest.Version < ManifestVersion {
		return manifest, nil
	}
	return loadProjectMetadata(root, manifest)
}

// loadProjectMetadata decrypts each projects/<id>/meta.age into the manifest,
// skipping directory names that are not ids. It records the marshaled bytes of
// every loaded project in baseline so SaveManifest can tell what changed.
func loadProjectMetadata(root string, manifest Manifest) (Manifest, error) {
	dir := filepath.Join(root, projectsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read projects directory: %w", err)
	}
	type metaFile struct {
		id   string
		path string
	}
	metas := make([]metaFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !isID(entry.Name()) {
			continue // foreign directories are left untouched
		}
		metaPath := filepath.Join(dir, entry.Name(), metadataName)
		info, err := os.Stat(metaPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("stat project metadata %s: %w", entry.Name(), err)
		}
		if info.IsDir() {
			continue
		}
		metas = append(metas, metaFile{id: entry.Name(), path: metaPath})
	}
	if len(metas) == 0 {
		return manifest, nil
	}
	identity, err := LoadIdentityForManifest(manifest)
	if err != nil {
		// Encrypted metadata exists but no available identity can read it. Seal
		// rather than fail so a locked launch can still prompt for unlock, and so
		// the next save cannot flatten a project set it never saw.
		manifest.Sealed = true
		return manifest, nil
	}
	for _, meta := range metas {
		ciphertext, err := os.ReadFile(meta.path)
		if err != nil {
			return Manifest{}, fmt.Errorf("read project metadata %s: %w", meta.id, err)
		}
		plaintext, err := Decrypt(ciphertext, identity)
		if err != nil {
			return Manifest{}, fmt.Errorf("decrypt project metadata %s: %w", meta.id, err)
		}
		entry, err := unmarshalProject(plaintext, meta.id)
		if err != nil {
			return Manifest{}, fmt.Errorf("parse project metadata %s: %w", meta.id, err)
		}
		if existing, ok := manifest.Projects[entry.Name]; ok {
			return Manifest{}, fmt.Errorf("duplicate project name %q in metadata %s and %s", entry.Name, existing.ID, meta.id)
		}
		manifest.Projects[entry.Name] = entry
		marshaled, err := marshalProject(entry)
		if err != nil {
			return Manifest{}, fmt.Errorf("marshal project metadata %s: %w", meta.id, err)
		}
		manifest.baseline[entry.Name] = marshaled
	}
	return manifest, nil
}

// SaveManifest writes the bootstrap gitenv.json and each changed project's
// encrypted metadata. It refuses a sealed manifest and refuses to write v3
// semantics into an older vault. Metadata is re-encrypted only when its bytes
// actually changed (age output is nondeterministic, so unconditional writes
// would rewrite every file on every save and reintroduce merge conflicts), and
// project directories no longer referenced by the manifest are pruned.
func SaveManifest(root string, manifest Manifest) error {
	if manifest.Sealed {
		return errors.New("refusing to save a sealed vault manifest: no identity could read existing metadata")
	}
	if manifest.Version != 0 && manifest.Version < ManifestVersion {
		return fmt.Errorf("refusing to write v%d metadata into a v%d vault; upgrade first", ManifestVersion, manifest.Version)
	}
	manifest.Version = ManifestVersion
	if manifest.Projects == nil {
		manifest.Projects = map[string]Project{}
	}
	if manifest.baseline == nil {
		manifest.baseline = map[string][]byte{}
	}

	var recipients []age.Recipient
	liveIDs := make(map[string]struct{}, len(manifest.Projects))
	for name := range manifest.Projects {
		id, err := manifest.EnsureProjectID(name)
		if err != nil {
			return fmt.Errorf("assign id for project %q: %w", name, err)
		}
		liveIDs[id] = struct{}{}
		entry := manifest.Projects[name]
		for profile := range entry.Profiles {
			if _, err := manifest.EnsureProfileID(name, profile); err != nil {
				return fmt.Errorf("assign id for profile %q of project %q: %w", profile, name, err)
			}
		}
		entry = manifest.Projects[name]
		marshaled, err := marshalProject(entry)
		if err != nil {
			return fmt.Errorf("marshal project %q: %w", name, err)
		}
		if bytes.Equal(marshaled, manifest.baseline[name]) {
			continue // unchanged: skip rewrite to avoid nondeterministic churn
		}
		if recipients == nil {
			recipients, err = ParseRecipients(manifest.Recipients)
			if err != nil {
				return err
			}
		}
		ciphertext, err := Encrypt(marshaled, recipients)
		if err != nil {
			return fmt.Errorf("encrypt project %q: %w", name, err)
		}
		metaPath := filepath.Join(root, filepath.FromSlash(metadataRelPath(id)))
		if err := WriteAtomic(metaPath, ciphertext, 0o600); err != nil {
			return fmt.Errorf("write metadata for project %q: %w", name, err)
		}
		manifest.baseline[name] = marshaled
	}

	if err := pruneProjectDirs(root, liveIDs); err != nil {
		return err
	}

	// Write gitenv.json last. Projects, Sealed and baseline are json:"-", so the
	// serialized bootstrap file carries only plaintext fields.
	return writeJSONAtomic(filepath.Join(root, manifestName), manifest, 0o600)
}

// pruneProjectDirs removes projects/<id> directories whose id is no longer
// referenced. Foreign directories (non-id names) are left untouched. It runs
// only after SaveManifest's sealed check, so liveIDs reflects every project the
// caller could actually read; a sealed manifest never reaches here.
func pruneProjectDirs(root string, liveIDs map[string]struct{}) error {
	dir := filepath.Join(root, projectsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read projects directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isID(entry.Name()) {
			continue
		}
		if _, ok := liveIDs[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove stale project %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// v2Manifest parses the version-2 bootstrap file for in-place upgrade. Version 2
// stored project metadata in plaintext under "projects" and recorded
// repositories as canonical identity strings.
type v2Manifest struct {
	Version    int                  `json:"version"`
	Recipients []string             `json:"recipients"`
	Projects   map[string]v2Project `json:"projects"`
}

type v2Project struct {
	Profiles     map[string]v2Profile `json:"profiles"`
	Repositories []string             `json:"repositories"`
}

type v2Profile struct {
	UpdatedAt time.Time `json:"updated_at"`
	Checksum  string    `json:"checksum"`
}

// NeedsUpgrade reports whether the vault's on-disk format predates
// ManifestVersion. It reads only the plaintext bootstrap file, so callers can
// decide whether an upgrade is pending before paying for any remote check.
func NeedsUpgrade(root string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return false, fmt.Errorf("read manifest: %w", err)
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("parse manifest: %w", err)
	}
	return probe.Version < ManifestVersion, nil
}

// UpgradeManifest migrates a version-2 vault to the version-3 layout. It writes
// each plaintext project as an encrypted projects/<id>/meta.age and copies every
// profile ciphertext under random ids, then rewrites gitenv.json at v3. It
// reports whether anything changed and is idempotent.
//
// No identity is required because recipients are stored in plaintext: metadata
// is only encrypted here, never decrypted.
//
// Crash safety. Profile ciphertexts are COPIED, never moved, and gitenv.json is
// rewritten only once the whole v3 layout exists on disk. That single write is
// the commit point:
//
//   - interrupted before it, the vault is still a complete, valid v2 vault and
//     the only residue is unreferenced id directories;
//   - interrupted after it, the vault is a complete, valid v3 vault and the only
//     residue is the old name-keyed directories.
//
// Either way no ciphertext is ever the sole copy in a location the manifest does
// not reference. Moving instead of copying would break this: a crash mid-upgrade
// would leave a v2 manifest pointing at files that no longer exist, and because
// every run draws fresh random ids the relocated copies would be unreachable.
// sweepUpgradeResidue clears both kinds of residue on the next run.
func UpgradeManifest(root string) (bool, error) {
	manifestPath := filepath.Join(root, manifestName)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		return false, fmt.Errorf("read manifest: %w", err)
	}
	var v2 v2Manifest
	if err := json.Unmarshal(original, &v2); err != nil {
		return false, fmt.Errorf("parse v2 manifest: %w", err)
	}
	swept, err := sweepUpgradeResidue(root, v2.Version >= ManifestVersion)
	if err != nil {
		return false, err
	}
	if v2.Version >= ManifestVersion {
		return swept, nil
	}
	if v2.Version != 2 {
		return false, fmt.Errorf("cannot upgrade vault version %d", v2.Version)
	}
	recipients, err := ParseRecipients(v2.Recipients)
	if err != nil {
		return false, err
	}

	oldDirs := make([]string, 0, len(v2.Projects))
	for name, project := range v2.Projects {
		projectID, err := newID()
		if err != nil {
			return false, err
		}
		entry := Project{ID: projectID, Name: name, Profiles: map[string]Profile{}}
		for _, identity := range project.Repositories {
			entry.Repositories = append(entry.Repositories, Repository{Identity: identity})
		}
		for profile, stored := range project.Profiles {
			profileID, err := newID()
			if err != nil {
				return false, err
			}
			oldPath := filepath.Join(root, projectsDir, name, profile+".env.age")
			newPath := filepath.Join(root, filepath.FromSlash(profileRelPath(projectID, profileID)))
			if err := copyCiphertext(oldPath, newPath); err != nil {
				return false, fmt.Errorf("copy profile %q/%q: %w", name, profile, err)
			}
			entry.Profiles[profile] = Profile{ID: profileID, UpdatedAt: stored.UpdatedAt, Checksum: stored.Checksum}
		}
		payload, err := marshalProject(entry)
		if err != nil {
			return false, fmt.Errorf("marshal project %q: %w", name, err)
		}
		ciphertext, err := Encrypt(payload, recipients)
		if err != nil {
			return false, fmt.Errorf("encrypt project %q: %w", name, err)
		}
		metaPath := filepath.Join(root, filepath.FromSlash(metadataRelPath(projectID)))
		if err := WriteAtomic(metaPath, ciphertext, 0o600); err != nil {
			return false, fmt.Errorf("write metadata for project %q: %w", name, err)
		}
		oldDirs = append(oldDirs, filepath.Join(root, projectsDir, name))
	}

	// Commit point: every v3 file exists, so switching the bootstrap version is
	// the single step that makes the new layout authoritative.
	if err := rewriteManifestToV3(manifestPath, original); err != nil {
		return false, err
	}

	for _, dir := range oldDirs {
		if err := os.RemoveAll(dir); err != nil {
			return false, fmt.Errorf("remove old project directory %s: %w", dir, err)
		}
	}
	return true, nil
}

// sweepUpgradeResidue deletes directories an interrupted upgrade left behind and
// reports whether it removed anything.
//
// An id directory without meta.age is unreferenced by definition: ids are random
// per run, so nothing can ever point at it again. A name-keyed directory is v2
// residue and is only removed once the manifest is v3, because before the commit
// point those directories are the authoritative ciphertexts.
//
// This runs at startup, before any capture can be in flight, which is why an
// id directory that has ciphertexts but no metadata is safe to treat as garbage.
func sweepUpgradeResidue(root string, upgraded bool) (bool, error) {
	dir := filepath.Join(root, projectsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read projects directory: %w", err)
	}
	removed := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		target := filepath.Join(dir, entry.Name())
		switch {
		case isID(entry.Name()):
			if _, statErr := os.Stat(filepath.Join(target, metadataName)); statErr == nil {
				continue
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return removed, fmt.Errorf("inspect project %s: %w", entry.Name(), statErr)
			}
		case !upgraded:
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return removed, fmt.Errorf("remove upgrade residue %s: %w", entry.Name(), err)
		}
		removed = true
	}
	return removed, nil
}

// rewriteManifestToV3 rewrites the bootstrap file at ManifestVersion, dropping
// the plaintext projects key and stamping the new version while keeping every
// other key intact (mirrors saveEnrollmentManifest's raw-map merge).
func rewriteManifestToV3(manifestPath string, original []byte) error {
	merged := make(map[string]json.RawMessage)
	if err := json.Unmarshal(original, &merged); err != nil {
		return fmt.Errorf("parse manifest for upgrade: %w", err)
	}
	delete(merged, "projects")
	version, err := json.Marshal(ManifestVersion)
	if err != nil {
		return err
	}
	merged["version"] = version
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteAtomic(manifestPath, data, 0o600)
}

// copyCiphertext duplicates a profile ciphertext into its v3 location, leaving
// the v2 original untouched so the pre-commit manifest stays valid, and verifies
// the copy byte-for-byte before returning.
//
// Verifying matters because the bytes are age ciphertext: a truncated copy would
// only surface much later as an undecryptable profile, long after the v2 original
// was cleaned up.
func copyCiphertext(oldPath, newPath string) error {
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	if err := WriteAtomic(newPath, data, 0o600); err != nil {
		return err
	}
	written, err := os.ReadFile(newPath)
	if err != nil {
		return fmt.Errorf("verify copy: %w", err)
	}
	if !bytes.Equal(data, written) {
		return fmt.Errorf("copy of %s is corrupt", filepath.Base(oldPath))
	}
	return nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteAtomic(path, data, mode)
}

func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gitenv-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(tmpName, path)
}

func Checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
