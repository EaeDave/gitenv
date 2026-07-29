package vault

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// DefaultEnvFile is the env file gitenv manages when a project does not name
// another one.
const DefaultEnvFile = ".env"

// EnvFileOf returns the project-relative env file in slash form, defaulting to
// DefaultEnvFile. Storing the slash form keeps the vault portable: a project
// captured on Windows resolves correctly on Linux and vice versa.
func EnvFileOf(entry Project) string {
	if strings.TrimSpace(entry.EnvFile) == "" {
		return DefaultEnvFile
	}
	return entry.EnvFile
}

// ProjectEntry returns a project's metadata for resolving its managed env file,
// failing closed when the manifest is sealed.
//
// Without this guard a sealed manifest hands back a zero Project, whose EnvFile
// is empty and therefore resolves to the default ".env". A project that manages
// "apps/web/.env" would then be read from — or worse, written to — the wrong
// path, silently, purely because the vault was locked.
//
// A project that is simply absent from an unsealed manifest is not an error: it
// is linked locally but has never been captured, and the default env file is the
// correct answer.
func ProjectEntry(manifest Manifest, project string) (Project, error) {
	if manifest.Sealed {
		return Project{}, errors.New("vault metadata is locked; unlock the vault first")
	}
	return manifest.Projects[project], nil
}

// ValidateEnvFile rejects anything that could write outside the project
// directory or that would not survive a round trip through another OS.
func ValidateEnvFile(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil // empty means the default
	}
	if strings.ContainsRune(trimmed, '\\') {
		return errors.New("env file path must use forward slashes")
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return errors.New("env file path contains control characters")
	}
	if path.IsAbs(trimmed) || strings.HasPrefix(trimmed, "~") {
		return errors.New("env file path must be relative to the project")
	}
	if len(trimmed) > 1 && trimmed[1] == ':' {
		return errors.New("env file path must be relative to the project")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("env file path must stay inside the project")
	}
	for _, element := range strings.Split(cleaned, "/") {
		if element == ".." {
			return errors.New("env file path must stay inside the project")
		}
	}
	return nil
}

// NormalizeEnvFile validates and canonicalizes a user-supplied env file path.
// It returns "" for the default so the field stays absent in stored metadata.
func NormalizeEnvFile(value string) (string, error) {
	if err := ValidateEnvFile(value); err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	cleaned := path.Clean(trimmed)
	if cleaned == DefaultEnvFile {
		return "", nil
	}
	return cleaned, nil
}

// EnvPath resolves a project's env file to an absolute host path.
func EnvPath(local LocalProject, entry Project) (string, error) {
	if local.Path == "" {
		return "", errors.New("project has no local path")
	}
	relative := EnvFileOf(entry)
	if err := ValidateEnvFile(relative); err != nil {
		return "", fmt.Errorf("project env file: %w", err)
	}
	return filepath.Join(local.Path, filepath.FromSlash(relative)), nil
}
