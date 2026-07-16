package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if manifest.Projects == nil {
		manifest.Projects = map[string]Project{}
	}
	return manifest, nil
}

func SaveManifest(root string, manifest Manifest) error {
	return writeJSONAtomic(filepath.Join(root, manifestName), manifest, 0o600)
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

func ProfilePath(root, project, profile string) string {
	return filepath.Join(root, "projects", project, profile+".env.age")
}
