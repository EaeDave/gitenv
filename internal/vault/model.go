package vault

import "time"

// ManifestVersion 3 moved every project-identifying field out of the plaintext
// manifest into per-project encrypted metadata files. Version 2 vaults are
// upgraded in place by UpgradeManifest.
const ManifestVersion = 3

// Manifest is the vault's bootstrap file (gitenv.json). Only fields that must
// be readable *without* the vault identity are serialized here: recipients, the
// password-wrapped identity, enrolled devices and pending enrollment requests.
//
// Project metadata lives in projects/<id>/meta.age, one encrypted file per
// project, so two devices adding different projects touch different files and
// still merge. LoadManifest decrypts them into Projects and SaveManifest
// re-encrypts only the entries that actually changed.
type Manifest struct {
	Version            int                 `json:"version"`
	Recipients         []string            `json:"recipients"`
	WrappedIdentity    *WrappedIdentity    `json:"wrapped_identity,omitempty"`
	Devices            []Device            `json:"devices,omitempty"`
	EnrollmentRequests []EnrollmentRequest `json:"enrollment_requests,omitempty"`

	// Projects holds decrypted project metadata keyed by project name. It is
	// never serialized into gitenv.json.
	Projects map[string]Project `json:"-"`

	// Sealed reports that encrypted metadata exists but could not be read with
	// the available identity. SaveManifest refuses to write a sealed manifest,
	// so a locked vault can never be flattened to an empty project set.
	Sealed bool `json:"-"`

	// baseline is the marshaled metadata of each project as it was loaded from
	// disk. SaveManifest re-encrypts a project only when its metadata differs,
	// which keeps age's nondeterministic output from rewriting every file on
	// every save.
	baseline map[string][]byte
}

// Project is both the in-memory project entry and the plaintext payload of
// projects/<ID>/meta.age.
//
// ID is the directory name and is therefore not part of the encrypted payload.
// Name is stored inside the ciphertext because the directory name is random:
// the id-to-name mapping must not be recoverable without the vault identity.
type Project struct {
	ID           string             `json:"-"`
	Name         string             `json:"name"`
	Profiles     map[string]Profile `json:"profiles"`
	Repositories []Repository       `json:"repositories,omitempty"`
	// EnvFile is the project-relative env file path in slash form. Empty means
	// the default ".env".
	EnvFile string `json:"env_file,omitempty"`
	// LineEndings selects how stored bytes map to on-disk bytes. Empty
	// preserves bytes exactly, which is the historical behavior.
	LineEndings LineEndingPolicy `json:"line_endings,omitempty"`
}

// Repository records one Git remote that maps to a project. Identity is the
// canonical comparison key produced by git.NormalizeRemoteURL. CloneURL is the
// credential-free remote URL as first observed, kept because the canonical
// identity is lower-cased and scheme-less and so not always cloneable.
type Repository struct {
	Identity string `json:"identity"`
	CloneURL string `json:"clone_url,omitempty"`
}

// Profile is one encrypted env snapshot. ID names the ciphertext file inside
// the project directory so profile names never appear on disk either.
type Profile struct {
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Checksum  string    `json:"checksum"`
}

type LocalConfig struct {
	VaultPath           string                  `json:"vault_path"`
	Projects            map[string]LocalProject `json:"projects"`
	PendingEnrollmentID string                  `json:"pending_enrollment_id,omitempty"`
	// WorkspaceRoot is where clones of missing projects are created. Empty
	// means "infer from the projects already linked on this computer".
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	// Discovery caches the last repository scan so launching the TUI never
	// walks the filesystem again unprompted.
	Discovery *DiscoveryCache `json:"discovery,omitempty"`
}

type LocalProject struct {
	Path               string `json:"path"`
	ActiveProfile      string `json:"active_profile,omitempty"`
	RepositoryIdentity string `json:"repository_identity,omitempty"`
}

// DiscoveryCache maps canonical repository identities to the local paths where
// the last scan found a clone with that origin remote.
type DiscoveryCache struct {
	ScannedAt time.Time           `json:"scanned_at"`
	Roots     []string            `json:"roots,omitempty"`
	Found     map[string][]string `json:"found,omitempty"`
}
