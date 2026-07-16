package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func RevisionFiles(root, revision string) (map[string][]byte, error) {
	output, err := run(root, "ls-tree", "-r", "--name-only", "-z", revision)
	if err != nil {
		return nil, fmt.Errorf("list revision files: %w", err)
	}

	files := make(map[string][]byte)
	for _, path := range nulSeparated(output) {
		content, err := run(root, "show", revision+":"+path)
		if err != nil {
			return nil, fmt.Errorf("read revision file %q: %w", path, err)
		}
		files[path] = []byte(content)
	}
	return files, nil
}

func WorktreeFiles(root string) (map[string][]byte, error) {
	output, err := run(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list worktree files: %w", err)
	}

	files := make(map[string][]byte)
	for _, path := range nulSeparated(output) {
		if !safeRelativePath(path) {
			return nil, fmt.Errorf("unsafe worktree path %q", path)
		}
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect worktree file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read worktree file %q: %w", path, err)
		}
		files[path] = content
	}
	return files, nil
}

func UpstreamRevision(root string) (string, error) {
	output, err := run(root, "rev-parse", "--verify", "@{upstream}")
	if err != nil {
		return "", fmt.Errorf("resolve upstream revision: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func HasHead(root string) bool {
	_, err := run(root, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func nulSeparated(output string) []string {
	parts := strings.Split(output, "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
