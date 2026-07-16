package git

import (
	"errors"
	"fmt"
	"strings"
)

func AddRemote(root, name, url string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
		return errors.New("remote name and URL are required")
	}
	if _, err := run(root, "remote", "add", "--", name, url); err != nil {
		return fmt.Errorf("add git remote: %w", err)
	}
	return nil
}

func RemoteURL(root, name string) (string, error) {
	output, err := run(root, "remote", "get-url", "--", name)
	if err != nil {
		return "", fmt.Errorf("read git remote: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func HasRemote(root, name string) bool {
	_, err := RemoteURL(root, name)
	return err == nil
}

func SetRemoteURL(root, name, url string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
		return errors.New("remote name and URL are required")
	}
	if _, err := run(root, "remote", "set-url", "--", name, url); err != nil {
		return fmt.Errorf("set git remote URL: %w", err)
	}
	return nil
}

func RemoveRemote(root, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("remote name is required")
	}
	if _, err := run(root, "remote", "remove", "--", name); err != nil {
		return fmt.Errorf("remove git remote: %w", err)
	}
	return nil
}

// PingRemote tests whether the named remote is reachable by connecting and
// listing its refs. It does not fetch or mutate any local state.
// Credentials embedded in the remote URL are redacted from any returned error.
func PingRemote(root, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("remote name is required")
	}
	if _, err := run(root, "ls-remote", "--quiet", "--", name); err != nil {
		return fmt.Errorf("ping git remote: %w", err)
	}
	return nil
}

func Root(path string) (string, error) {
	output, err := run(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("detect git root: %w", err)
	}
	return strings.TrimSpace(output), nil
}
