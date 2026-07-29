package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"regexp"
)

const (
	projectsDir   = "projects"
	metadataName  = "meta.age"
	ciphertextExt = ".age"
)

// idPattern matches the random directory and file identifiers used by the v3
// layout. Anything else under projects/ is foreign and left untouched.
var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// newID returns a random 128-bit identifier. Identifiers carry no information
// about the project or profile they name: the mapping lives inside the
// encrypted metadata, so reading the vault repository reveals only how many
// projects and profiles exist.
func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isID(value string) bool { return idPattern.MatchString(value) }

// projectRelDir returns the slash-separated vault-relative directory of a
// project id, for use as a Git snapshot key.
func projectRelDir(id string) string { return path.Join(projectsDir, id) }

// metadataRelPath returns the slash-separated vault-relative path of a
// project's encrypted metadata.
func metadataRelPath(id string) string { return path.Join(projectsDir, id, metadataName) }

// profileRelPath returns the slash-separated vault-relative path of a profile
// ciphertext.
func profileRelPath(projectID, profileID string) string {
	return path.Join(projectsDir, projectID, profileID+ciphertextExt)
}

// ProjectDir returns the absolute directory holding a project's encrypted
// metadata and profile ciphertexts.
func ProjectDir(root string, manifest Manifest, project string) (string, bool) {
	entry, ok := manifest.Projects[project]
	if !ok || entry.ID == "" {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(projectRelDir(entry.ID))), true
}

// ProfilePath returns the absolute path of a profile's ciphertext. It reports
// false when the project or profile has no identifier yet; callers that are
// creating a profile must call EnsureProfileID first.
func ProfilePath(root string, manifest Manifest, project, profile string) (string, bool) {
	entry, ok := manifest.Projects[project]
	if !ok || entry.ID == "" {
		return "", false
	}
	stored, ok := entry.Profiles[profile]
	if !ok || stored.ID == "" {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(profileRelPath(entry.ID, stored.ID))), true
}

// EnsureProjectID returns the project's identifier, creating the project entry
// and a fresh identifier when either is missing.
func (m *Manifest) EnsureProjectID(project string) (string, error) {
	if m.Projects == nil {
		m.Projects = map[string]Project{}
	}
	entry := m.Projects[project]
	entry.Name = project
	if entry.Profiles == nil {
		entry.Profiles = map[string]Profile{}
	}
	if entry.ID == "" {
		id, err := newID()
		if err != nil {
			return "", err
		}
		entry.ID = id
	}
	m.Projects[project] = entry
	return entry.ID, nil
}

// EnsureProfileID returns the profile's identifier, creating the project and
// profile entries and a fresh identifier when any of them is missing. The
// returned Profile still needs its checksum and timestamp set by the caller.
func (m *Manifest) EnsureProfileID(project, profile string) (string, error) {
	if _, err := m.EnsureProjectID(project); err != nil {
		return "", err
	}
	entry := m.Projects[project]
	stored := entry.Profiles[profile]
	if stored.ID == "" {
		id, err := newID()
		if err != nil {
			return "", err
		}
		stored.ID = id
		entry.Profiles[profile] = stored
		m.Projects[project] = entry
	}
	return stored.ID, nil
}

// marshalProject serializes a project's encrypted payload. Map keys are sorted
// by encoding/json, so equal metadata always produces equal bytes and
// SaveManifest can skip unchanged projects.
func marshalProject(entry Project) ([]byte, error) {
	if entry.Name == "" {
		return nil, errors.New("project metadata has no name")
	}
	if entry.Profiles == nil {
		entry.Profiles = map[string]Profile{}
	}
	return json.Marshal(entry)
}

func unmarshalProject(data []byte, id string) (Project, error) {
	var entry Project
	if err := json.Unmarshal(data, &entry); err != nil {
		return Project{}, err
	}
	if entry.Name == "" {
		return Project{}, errors.New("project metadata has no name")
	}
	if entry.Profiles == nil {
		entry.Profiles = map[string]Profile{}
	}
	entry.ID = id
	return entry, nil
}
