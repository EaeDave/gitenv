package git

import (
	"strings"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// SCP-like SSH
		{"scp-like with user and .git", "git@github.com:owner/repo.git", "github.com/owner/repo"},
		{"scp-like without user", "github.com:owner/repo.git", "github.com/owner/repo"},
		{"scp-like no .git suffix", "git@github.com:owner/repo", "github.com/owner/repo"},
		// Explicit ssh:// scheme
		{"ssh scheme with user", "ssh://git@github.com/owner/repo.git", "github.com/owner/repo"},
		{"ssh scheme no user", "ssh://github.com/owner/repo.git", "github.com/owner/repo"},
		// HTTPS
		{"https with .git", "https://github.com/owner/repo.git", "github.com/owner/repo"},
		{"https without .git", "https://github.com/owner/repo", "github.com/owner/repo"},
		{"https trailing slash", "https://github.com/owner/repo/", "github.com/owner/repo"},
		// HTTP
		{"http scheme", "http://github.com/owner/repo.git", "github.com/owner/repo"},
		// git:// scheme
		{"git scheme", "git://github.com/owner/repo.git", "github.com/owner/repo"},
		// Credentials stripped
		{"https user:pass", "https://user:secret@github.com/owner/repo.git", "github.com/owner/repo"},
		{"https token only", "https://mytoken@github.com/owner/repo.git", "github.com/owner/repo"},
		// Case-insensitive host
		{"uppercase host", "https://GitHub.COM/Owner/Repo.git", "github.com/Owner/Repo"},
		// Port in URL
		{"https with port", "https://github.com:443/owner/repo.git", "github.com/owner/repo"},
		{"https with custom port", "https://git.example.com:8443/owner/repo.git", "git.example.com:8443/owner/repo"},
		// Distinct repos that share a basename
		{"org-a same basename", "https://github.com/org-a/myapp.git", "github.com/org-a/myapp"},
		{"org-b same basename", "https://github.com/org-b/myapp.git", "github.com/org-b/myapp"},
		// Unrecognized / empty / local
		{"empty string", "", ""},
		{"absolute local path", "/home/user/repo", ""},
		{"relative path", "./repo", ""},
		{"file scheme", "file:///home/user/repo", ""},
		{"unknown scheme", "ftp://example.com/repo", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeRemoteURL(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeSSHEqualsHTTPS proves SSH and HTTPS forms of the same
// repository produce identical canonical identities.
func TestNormalizeSSHEqualsHTTPS(t *testing.T) {
	pairs := [][2]string{
		{
			"git@github.com:owner/repo.git",
			"https://github.com/owner/repo.git",
		},
		{
			"ssh://git@gitlab.com/group/project.git",
			"https://gitlab.com/group/project",
		},
		{
			"git@bitbucket.org:team/service.git",
			"http://bitbucket.org/team/service.git",
		},
	}

	for _, pair := range pairs {
		ssh, https := NormalizeRemoteURL(pair[0]), NormalizeRemoteURL(pair[1])
		if ssh == "" || https == "" {
			t.Errorf("normalization returned empty: ssh=%q https=%q", ssh, https)
			continue
		}
		if ssh != https {
			t.Errorf("SSH %q normalizes to %q; HTTPS %q normalizes to %q — must be equal",
				pair[0], ssh, pair[1], https)
		}
	}
}

// TestNormalizeCredentialsStripped proves that credentials embedded in a
// URL do not leak into the canonical identity.
func TestNormalizeCredentialsStripped(t *testing.T) {
	urls := []string{
		"https://user:s3cr3t@github.com/owner/repo.git",
		"https://token123@github.com/owner/repo.git",
	}
	bare := NormalizeRemoteURL("https://github.com/owner/repo.git")
	if bare == "" {
		t.Fatal("bare URL normalization returned empty string")
	}

	for _, raw := range urls {
		got := NormalizeRemoteURL(raw)
		if got != bare {
			t.Errorf("NormalizeRemoteURL(%q) = %q, want %q (credentials leaked or wrong result)", raw, got, bare)
		}
		// Belt-and-suspenders: ensure none of the credential tokens appear literally.
		for _, secret := range []string{"user", "s3cr3t", "token123"} {
			if strings.Contains(got, secret) {
				t.Errorf("credential %q leaked into identity %q", secret, got)
			}
		}
	}
}

// TestNormalizeSameBasenameDistinctOrgs proves that two repos sharing only
// their basename are considered distinct identities.
func TestNormalizeSameBasenameDistinctOrgs(t *testing.T) {
	cases := [][2]string{
		{
			"https://github.com/org-a/myapp.git",
			"https://github.com/org-b/myapp.git",
		},
		{
			"git@github.com:alice/service.git",
			"git@github.com:bob/service.git",
		},
	}

	for _, pair := range cases {
		a, b := NormalizeRemoteURL(pair[0]), NormalizeRemoteURL(pair[1])
		if a == "" || b == "" {
			t.Errorf("unexpected empty: a=%q b=%q", a, b)
			continue
		}
		if a == b {
			t.Errorf("same-basename different-org repos returned identical identity %q for both %q and %q",
				a, pair[0], pair[1])
		}
	}
}

func TestRedactURLCredentials(t *testing.T) {
	got := RedactURL("https://user:secret@example.com/org/repo.git")
	if got != "https://***@example.com/org/repo.git" || strings.Contains(got, "secret") {
		t.Fatalf("credentials exposed: %q", got)
	}
}
