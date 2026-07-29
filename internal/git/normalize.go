package git

import (
	"net/url"
	"strings"
)

// NormalizeRemoteURL returns the canonical repository identity for a Git
// remote URL. SSH (scp-like and ssh://), HTTPS, HTTP and git:// URLs all
// reduce to "host[:port]/path", lower-cased, with credentials, .git suffix, and
// trailing slashes removed. Returns "" for unrecognized or empty input.
//
// Two remote URLs that point to the same hosted repository always return the
// same non-empty string, so callers can compare with ==.
//
// The path is lower-cased along with the host because every major forge treats
// owner/repo case-insensitively: one machine configured with
// "git@github.com:EaeDave/app.git" and another with
// "https://github.com/eaedave/app.git" hold the same repository and must match,
// otherwise a project is never recognized on a freshly set up computer. The
// consequence is that this value is a comparison key, not a clone URL — the
// case-accurate URL is stored separately as Repository.CloneURL.
func NormalizeRemoteURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	var host, path string

	if !strings.Contains(rawURL, "://") {
		// SCP-like syntax: [user@]host:path/to/repo.git
		// Reject absolute local paths and relative paths without a host part.
		if strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, ".") {
			return ""
		}
		idx := strings.IndexByte(rawURL, ':')
		if idx <= 0 {
			return ""
		}
		hostPart := rawURL[:idx]
		path = rawURL[idx+1:]
		// Strip user@ prefix from the host component.
		if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		host = hostPart
	} else {
		u, err := url.Parse(rawURL)
		if err != nil {
			return ""
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "ssh", "https", "http", "git":
			host = u.Hostname()
			port := u.Port()
			if port != "" && !isDefaultGitPort(scheme, port) {
				host += ":" + port
			}
			path = u.Path
		default:
			return ""
		}
	}

	// Strip leading slash, .git suffix, and trailing slashes.
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimRight(path, "/")

	if host == "" || path == "" {
		return ""
	}

	return strings.ToLower(host + "/" + path)
}
func isDefaultGitPort(scheme, port string) bool {
	return (scheme == "ssh" && port == "22") ||
		(scheme == "https" && port == "443") ||
		(scheme == "http" && port == "80") ||
		(scheme == "git" && port == "9418")
}
