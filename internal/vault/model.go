package vault

import "time"

const ManifestVersion = 1

type Manifest struct {
	Version    int                `json:"version"`
	Recipients []string           `json:"recipients"`
	Projects   map[string]Project `json:"projects"`
}

type Project struct {
	Profiles map[string]Profile `json:"profiles"`
}

type Profile struct {
	UpdatedAt time.Time `json:"updated_at"`
	Checksum  string    `json:"checksum"`
}

type LocalConfig struct {
	VaultPath string                  `json:"vault_path"`
	Projects  map[string]LocalProject `json:"projects"`
}

type LocalProject struct {
	Path          string `json:"path"`
	ActiveProfile string `json:"active_profile,omitempty"`
}
