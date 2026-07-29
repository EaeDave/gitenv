package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func Init(root string, recipient string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	manifestPath := filepath.Join(root, manifestName)
	if _, err := os.Stat(manifestPath); err == nil {
		return fmt.Errorf("vault already initialized at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{recipient},
		Projects:   map[string]Project{},
	}
	if err := SaveManifest(root, manifest); err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(root, ".gitignore"), []byte("# Plaintext env files must never live in this vault.\n*.plaintext\nidentity.txt\n"), 0o600)
}

func Link(cfg *LocalConfig, project, path string) error {
	if err := ValidateName("project", project); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("project path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory: %s", absolute)
	}
	cfg.Projects[project] = LocalProject{Path: absolute, ActiveProfile: cfg.Projects[project].ActiveProfile, RepositoryIdentity: cfg.Projects[project].RepositoryIdentity}
	return nil
}

func Capture(cfg *LocalConfig, project, profile string) error {
	if err := ValidateName("project", project); err != nil {
		return err
	}
	if err := ValidateName("profile", profile); err != nil {
		return err
	}
	local, ok := cfg.Projects[project]
	if !ok {
		return fmt.Errorf("project %q is not linked on this computer", project)
	}
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	metadata, err := ProjectEntry(manifest, project)
	if err != nil {
		return err
	}
	envPath, err := EnvPath(local, metadata)
	if err != nil {
		return err
	}
	plaintext, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("read project env file: %w", err)
	}
	// Store canonical bytes: normalizing before both encryption and checksum
	// keeps a converting policy from reporting the captured file as modified.
	canonical := NormalizeStored(plaintext, metadata.LineEndings)
	recipients, err := ParseRecipients(manifest.Recipients)
	if err != nil {
		return err
	}
	ciphertext, err := Encrypt(canonical, recipients)
	if err != nil {
		return err
	}
	// Ensure the profile carries an id before resolving its ciphertext path;
	// the random id names the file so profile names never appear on disk.
	if _, err := manifest.EnsureProfileID(project, profile); err != nil {
		return err
	}
	profilePath, ok := ProfilePath(cfg.VaultPath, manifest, project, profile)
	if !ok {
		return fmt.Errorf("resolve ciphertext path for profile %q of project %q", profile, project)
	}
	if err := WriteAtomic(profilePath, ciphertext, 0o600); err != nil {
		return err
	}
	entry := manifest.Projects[project]
	stored := entry.Profiles[profile]
	stored.UpdatedAt = time.Now().UTC()
	stored.Checksum = Checksum(canonical)
	entry.Profiles[profile] = stored
	manifest.Projects[project] = entry
	if err := SaveManifest(cfg.VaultPath, manifest); err != nil {
		return err
	}
	local.ActiveProfile = profile
	cfg.Projects[project] = local
	return SaveLocal(*cfg)
}

func Apply(cfg *LocalConfig, project, profile string, force bool) error {
	local, ok := cfg.Projects[project]
	if !ok {
		return fmt.Errorf("project %q is not linked on this computer", project)
	}
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	projectEntry, ok := manifest.Projects[project]
	if !ok {
		return fmt.Errorf("project %q does not exist in vault", project)
	}
	target, err := EnvPath(local, projectEntry)
	if err != nil {
		return err
	}
	if existing, readErr := os.ReadFile(target); readErr == nil && !force {
		if local.ActiveProfile == "" {
			return errors.New("local .env is unmanaged; capture it or use --force")
		}
		active, exists := projectEntry.Profiles[local.ActiveProfile]
		// Compare against canonical bytes so a CRLF working copy under a
		// converting policy is not mistaken for an uncaptured change.
		if !exists || Checksum(NormalizeStored(existing, projectEntry.LineEndings)) != active.Checksum {
			return errors.New("local .env has uncaptured changes; capture it or use --force")
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	plaintext, err := ReadProfile(cfg, project, profile)
	if err != nil {
		return err
	}
	// A fresh clone may name a nested env path (apps/web/.env) whose parent
	// directory does not exist yet, so create it before writing.
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create env file directory: %w", err)
	}
	if err := WriteAtomic(target, RenderForDisk(plaintext, projectEntry.LineEndings), 0o600); err != nil {
		return err
	}
	local.ActiveProfile = profile
	cfg.Projects[project] = local
	return SaveLocal(*cfg)
}

// ReadProfile decrypts a profile and verifies its manifest checksum before
// returning plaintext to an explicit in-memory operation such as apply or preview.
func ReadProfile(cfg *LocalConfig, project, profile string) ([]byte, error) {
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return nil, err
	}
	projectEntry, ok := manifest.Projects[project]
	if !ok {
		return nil, fmt.Errorf("project %q does not exist in vault", project)
	}
	profileEntry, ok := projectEntry.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("profile %q does not exist for project %q", profile, project)
	}
	profilePath, ok := ProfilePath(cfg.VaultPath, manifest, project, profile)
	if !ok {
		return nil, fmt.Errorf("resolve ciphertext path for profile %q of project %q", profile, project)
	}
	ciphertext, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("read encrypted profile: %w", err)
	}
	identity, err := LoadIdentity()
	if err != nil {
		return nil, err
	}
	plaintext, err := Decrypt(ciphertext, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt profile: %w", err)
	}
	if Checksum(plaintext) != profileEntry.Checksum {
		return nil, errors.New("decrypted profile checksum mismatch")
	}
	return plaintext, nil
}

func RemoveProfile(cfg *LocalConfig, project, profile string) error {
	if err := ValidateName("project", project); err != nil {
		return err
	}
	if err := ValidateName("profile", profile); err != nil {
		return err
	}
	local, linked := cfg.Projects[project]
	if linked && local.ActiveProfile == profile {
		return fmt.Errorf("profile %q is active; apply another profile before removing it", profile)
	}
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	entry, exists := manifest.Projects[project]
	if !exists {
		return fmt.Errorf("project %q does not exist in vault", project)
	}
	if _, exists := entry.Profiles[profile]; !exists {
		return fmt.Errorf("profile %q does not exist for project %q", profile, project)
	}
	profilePath, ok := ProfilePath(cfg.VaultPath, manifest, project, profile)
	if !ok {
		return fmt.Errorf("resolve ciphertext path for profile %q of project %q", profile, project)
	}
	backupPath := profilePath + ".removing"
	if err := os.Rename(profilePath, backupPath); err != nil {
		return fmt.Errorf("stage profile removal: %w", err)
	}
	delete(entry.Profiles, profile)
	manifest.Projects[project] = entry
	if err := SaveManifest(cfg.VaultPath, manifest); err != nil {
		if restoreErr := os.Rename(backupPath, profilePath); restoreErr != nil {
			return fmt.Errorf("save manifest: %v; restore profile: %w", err, restoreErr)
		}
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove encrypted profile backup: %w", err)
	}
	return nil
}

func Status(cfg LocalConfig, project string) (string, error) {
	local, ok := cfg.Projects[project]
	if !ok {
		return "unlinked", nil
	}
	if local.ActiveProfile == "" {
		return "unmanaged", nil
	}
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return "", err
	}
	projectEntry, err := ProjectEntry(manifest, project)
	if err != nil {
		return "", err
	}
	profile, ok := projectEntry.Profiles[local.ActiveProfile]
	if !ok {
		return "missing", nil
	}
	envPath, err := EnvPath(local, projectEntry)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(envPath)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if Checksum(NormalizeStored(data, projectEntry.LineEndings)) == profile.Checksum {
		return "clean", nil
	}
	return "modified", nil
}

// ProfileStatuses reports how the live local .env relates to each stored
// profile of a project. Only the active profile can be "modified" (uncaptured
// local changes); inactive profiles are intact stored snapshots and are marked
// "current" only when the on-disk .env happens to match their bytes.
func ProfileStatuses(cfg LocalConfig, project string) (map[string]string, error) {
	local, ok := cfg.Projects[project]
	if !ok {
		return nil, nil
	}
	manifest, err := LoadManifest(cfg.VaultPath)
	if err != nil {
		return nil, err
	}
	entry, ok := manifest.Projects[project]
	if !ok {
		return nil, nil
	}
	envPath, err := EnvPath(local, entry)
	if err != nil {
		return nil, err
	}
	data, readErr := os.ReadFile(envPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	hasEnv := readErr == nil
	localSum := ""
	if hasEnv {
		localSum = Checksum(NormalizeStored(data, entry.LineEndings))
	}
	statuses := make(map[string]string, len(entry.Profiles))
	for name, profile := range entry.Profiles {
		switch {
		case name == local.ActiveProfile && !hasEnv:
			statuses[name] = "missing"
		case name == local.ActiveProfile && profile.Checksum == localSum:
			statuses[name] = "clean"
		case name == local.ActiveProfile:
			statuses[name] = "modified"
		case hasEnv && profile.Checksum == localSum:
			statuses[name] = "current"
		default:
			statuses[name] = ""
		}
	}
	return statuses, nil
}
