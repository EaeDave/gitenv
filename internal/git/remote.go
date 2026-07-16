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

func Root(path string) (string, error) {
	output, err := run(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("detect git root: %w", err)
	}
	return strings.TrimSpace(output), nil
}
