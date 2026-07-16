package vault

import "time"

const ManifestVersion = 2

type Manifest struct {
	Version            int                 `json:"version"`
	Recipients         []string            `json:"recipients"`
	Projects           map[string]Project  `json:"projects"`
	WrappedIdentity    *WrappedIdentity    `json:"wrapped_identity,omitempty"`
	Devices            []Device            `json:"devices,omitempty"`
	EnrollmentRequests []EnrollmentRequest `json:"enrollment_requests,omitempty"`
}

type Project struct {
	Profiles     map[string]Profile `json:"profiles"`
	Repositories []string           `json:"repositories,omitempty"`
}

type Profile struct {
	UpdatedAt time.Time `json:"updated_at"`
	Checksum  string    `json:"checksum"`
}

type LocalConfig struct {
	VaultPath           string                  `json:"vault_path"`
	Projects            map[string]LocalProject `json:"projects"`
	PendingEnrollmentID string                  `json:"pending_enrollment_id,omitempty"`
}

type LocalProject struct {
	Path               string `json:"path"`
	ActiveProfile      string `json:"active_profile,omitempty"`
	RepositoryIdentity string `json:"repository_identity,omitempty"`
}
