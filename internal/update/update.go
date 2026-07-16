// Package update implements gitenv's self-updater against GitHub Releases.
//
// It reuses the release layout produced by scripts/build-release.sh: one static
// binary per platform named gitenv_<os>_<arch>[.exe] plus a checksums.txt. The
// updater downloads the matching asset for the running platform, verifies its
// SHA-256 checksum, replaces the running executable in place, and (optionally)
// re-launches it. The in-place replace and relaunch are platform specific and
// live in update_unix.go / update_windows.go.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "EaeDave/gitenv"

// DisabledEnv, when set to a non-empty value, turns off the automatic update
// check performed by the TUI on startup.
const DisabledEnv = "GITENV_NO_UPDATE"

// Disabled reports whether the automatic update check is opted out.
func Disabled() bool { return os.Getenv(DisabledEnv) != "" }

// assetName is the release asset for the running platform.
func assetName() string {
	name := fmt.Sprintf("gitenv_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// LatestTag returns the newest release tag by reading the redirect target of
// the releases/latest page. This avoids the GitHub API's unauthenticated rate
// limit and uses the same host the release assets are downloaded from.
func LatestTag(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	url := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gitenv-updater")
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("releases/latest did not redirect (status %s)", resp.Status)
	}
	idx := strings.LastIndex(loc, "/tag/")
	if idx < 0 {
		return "", fmt.Errorf("no published release found")
	}
	tag := strings.TrimSpace(loc[idx+len("/tag/"):])
	if tag == "" {
		return "", fmt.Errorf("no published release found")
	}
	return tag, nil
}

// Check resolves the latest tag and reports whether it is newer than current.
// A non-release current version (for example a local "dev" build) never reports
// an available update.
func Check(ctx context.Context, current string) (latest string, newer bool, err error) {
	latest, err = LatestTag(ctx)
	if err != nil {
		return "", false, err
	}
	return latest, IsNewer(current, latest), nil
}

// IsRelease reports whether current looks like a published release version
// (rather than a local "dev" build), i.e. whether self-update applies.
func IsRelease(current string) bool {
	_, ok := parseVersion(current)
	return ok
}

// RunCLI performs `gitenv --update`: check the latest release, and install it
// when it is newer than current (or when force is set).
func RunCLI(current string, force bool) error {
	fmt.Printf("==> current version: %s\n", current)
	fmt.Println("==> checking GitHub releases")
	latest, err := LatestTag(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("==> latest release: %s\n", latest)

	if !force {
		if _, ok := parseVersion(current); !ok {
			fmt.Printf("==> %s is a development build; use --force to install %s\n", current, latest)
			return nil
		}
		if !IsNewer(current, latest) {
			fmt.Printf("==> already up to date (%s)\n", current)
			return nil
		}
	}

	fmt.Printf("==> downloading %s (%s)\n", assetName(), latest)
	path, err := Apply(context.Background(), latest)
	if err != nil {
		return err
	}
	fmt.Printf("==> updated to %s at %s\n", latest, path)
	fmt.Println("==> restart gitenv to use the new version")
	return nil
}

// IsNewer reports whether latest is a strictly higher release than current.
func IsNewer(current, latest string) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return false // dev / unknown builds never auto-update
	}
	next, ok := parseVersion(latest)
	if !ok {
		return false
	}
	return next.greater(cur)
}

type semver struct{ major, minor, patch int }

func (v semver) greater(o semver) bool {
	if v.major != o.major {
		return v.major > o.major
	}
	if v.minor != o.minor {
		return v.minor > o.minor
	}
	return v.patch > o.patch
}

// parseVersion accepts "v1.2.3" or "1.2.3" (ignoring any pre-release suffix).
func parseVersion(s string) (semver, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return semver{}, false
	}
	// Drop a pre-release/build suffix such as -rc1 or +meta.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, false
	}
	var out [3]int
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return semver{}, false
		}
		out[i] = n
	}
	return semver{out[0], out[1], out[2]}, true
}

// Apply downloads the release binary for tag, verifies its checksum, and
// replaces the running executable in place. It returns the installed path.
func Apply(ctx context.Context, tag string) (string, error) {
	dest, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	dir := filepath.Dir(dest)
	if err := writable(dir); err != nil {
		return "", fmt.Errorf("%s is not writable; reinstall gitenv or move it to a user-writable directory: %w", dir, err)
	}

	asset := assetName()
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, tag)

	newPath := dest + ".new"
	_ = os.Remove(newPath)
	if err := downloadFile(ctx, base+"/"+asset, newPath); err != nil {
		return "", fmt.Errorf("download %s: %w", asset, err)
	}

	sums, err := downloadText(ctx, base+"/checksums.txt")
	if err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("download checksums: %w", err)
	}
	want, ok := checksumFor(sums, asset)
	if !ok {
		os.Remove(newPath)
		return "", fmt.Errorf("no checksum listed for %s", asset)
	}
	got, err := sha256File(newPath)
	if err != nil {
		os.Remove(newPath)
		return "", err
	}
	if !strings.EqualFold(got, want) {
		os.Remove(newPath)
		return "", fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, got)
	}

	if err := setExecutable(newPath); err != nil {
		os.Remove(newPath)
		return "", err
	}
	if err := swapBinary(newPath, dest); err != nil {
		os.Remove(newPath)
		return "", err
	}
	return dest, nil
}

func checksumFor(sums, asset string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], true
		}
	}
	return "", false
}

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Linux reports a replaced-in-place binary as "/path (deleted)".
	exe = strings.TrimSuffix(exe, " (deleted)")
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

func writable(dir string) error {
	probe, err := os.CreateTemp(dir, ".gitenv-write-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

func downloadFile(ctx context.Context, url, dest string) error {
	body, err := get(ctx, url, 90*time.Second)
	if err != nil {
		return err
	}
	defer body.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func downloadText(ctx context.Context, url string) (string, error) {
	body, err := get(ctx, url, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func get(ctx context.Context, url string, timeout time.Duration) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", "gitenv-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return &cancelReadCloser{rc: resp.Body, cancel: cancel}, nil
}

type cancelReadCloser struct {
	rc     io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Read(p []byte) (int, error) { return c.rc.Read(p) }
func (c *cancelReadCloser) Close() error {
	err := c.rc.Close()
	c.cancel()
	return err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
