package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	gitops "github.com/eaedave/gitenv/internal/git"
)

// Candidate is a git repository found on disk together with the canonical
// identity derived from its origin remote.
type Candidate struct {
	Path     string // absolute repository root
	Identity string // git.NormalizeRemoteURL of its origin remote
}

// ScanOptions tunes ScanRepositories. Zero values are replaced with defaults.
type ScanOptions struct {
	Roots    []string
	MaxDepth int
	Workers  int
}

// OriginFromGitDir returns the canonical identity of a repository's origin
// remote by parsing its git config file directly. It deliberately does not
// shell out to `git remote get-url`: the scanner visits thousands of
// directories and one process spawn per repository makes the scan unusable,
// while reading a small text file is roughly a thousand times cheaper.
//
// It handles `.git` being a directory (the common case) and `.git` being a
// file whose "gitdir:" line points elsewhere (worktrees and submodules),
// following a linked worktree's `commondir` to the shared repository.
//
// An origin url that fails to normalize yields "" with a nil error, so the
// caller can treat it as "not a candidate" without special-casing.
func OriginFromGitDir(repoRoot string) (string, error) {
	configPath, err := gitConfigPath(repoRoot)
	if err != nil {
		return "", err
	}
	raw, err := originURLFromConfig(configPath)
	if err != nil {
		return "", err
	}
	return gitops.NormalizeRemoteURL(raw), nil
}

// gitConfigPath resolves the path to the git config that holds the remotes for
// repoRoot, transparently following a `.git` file to the real git directory.
func gitConfigPath(repoRoot string) (string, error) {
	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("no .git in %s: %w", repoRoot, err)
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "config"), nil
	}

	// `.git` is a file: "gitdir: <path>". Worktrees and submodules use this.
	gitDir, err := resolveGitdirFile(gitPath, repoRoot)
	if err != nil {
		return "", err
	}

	// A linked worktree's gitdir has a `commondir` file pointing at the shared
	// repository whose config carries the remotes; follow it when present.
	commonDir := gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		rel := strings.TrimSpace(string(data))
		if rel != "" {
			if filepath.IsAbs(rel) {
				commonDir = filepath.Clean(rel)
			} else {
				commonDir = filepath.Clean(filepath.Join(gitDir, rel))
			}
		}
	}
	return filepath.Join(commonDir, "config"), nil
}

// resolveGitdirFile reads a `.git` file and returns the absolute git directory
// it names, resolving a relative gitdir against repoRoot.
func resolveGitdirFile(gitFile, repoRoot string) (string, error) {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return "", fmt.Errorf("read .git file in %s: %w", repoRoot, err)
	}
	var target string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "gitdir:"); ok {
			target = strings.TrimSpace(rest)
			break
		}
	}
	if target == "" {
		return "", fmt.Errorf("malformed .git file in %s: no gitdir", repoRoot)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Clean(filepath.Join(repoRoot, target))
	}
	return target, nil
}

// originURLFromConfig parses an INI-ish git config and returns the raw url of
// the [remote "origin"] section. Section headers may carry arbitrary
// surrounding whitespace, `url=x` and `url = x` are both accepted, `;` and `#`
// introduce comments, and scanning stops at the next section header.
func originURLFromConfig(configPath string) (string, error) {
	f, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("open git config: %w", err)
	}
	defer f.Close()

	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inOrigin = isOriginSection(line)
			continue
		}
		if !inOrigin {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "url") {
			return parseConfigValue(val), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read git config %s: %w", configPath, err)
	}
	return "", fmt.Errorf("no origin url in %s", configPath)
}

// isOriginSection reports whether a `[...]` header names the origin remote.
// Subsection names are case-sensitive in git, so "origin" is matched exactly.
func isOriginSection(line string) bool {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return false
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	rest, ok := cutFoldPrefix(inner, "remote")
	if !ok {
		return false
	}
	return strings.TrimSpace(rest) == `"origin"`
}

// cutFoldPrefix trims a case-insensitive prefix, reporting whether it matched.
func cutFoldPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// parseConfigValue trims a config value, honouring a quoted form and stripping
// an unquoted inline comment.
func parseConfigValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.IndexByte(v[1:], '"'); end >= 0 {
			return v[1 : 1+end]
		}
		return v[1:]
	}
	for i := range len(v) {
		if v[i] == '#' || v[i] == ';' {
			return strings.TrimSpace(v[:i])
		}
	}
	return v
}

// DefaultScanRoots returns the OS-aware, deduplicated set of existing absolute
// directories worth scanning for clones. It intentionally excludes filesystem
// roots ("/" and "C:\") — a full-disk walk is neither bounded nor useful — and
// puts the home directory last as a deep-but-pruned fallback.
func DefaultScanRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	var names []string
	if runtime.GOOS == "windows" {
		names = []string{
			filepath.Join("source", "repos"), // Visual Studio default
			"dev", "code", "projects", "repos",
			filepath.Join("Documents", "GitHub"),
		}
	} else {
		names = []string{
			"dev", "src", "code", "codigo", "projects", "repos",
			"work", "workspace",
			filepath.Join("go", "src"),
			"git", "IdeaProjects",
			filepath.Join("Documents", "GitHub"),
		}
	}

	var roots []string
	seen := make(map[string]bool)
	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		abs = filepath.Clean(abs)
		key := abs
		if caseInsensitiveFS {
			key = strings.ToLower(abs)
		}
		if seen[key] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return
		}
		seen[key] = true
		roots = append(roots, abs)
	}

	for _, n := range names {
		add(filepath.Join(home, n))
	}
	add(home) // home itself, last
	return roots
}

// ScanRepositories walks the configured roots with a bounded worker pool and
// returns the git repositories found, each with the canonical identity of its
// origin. It stops descending as soon as a directory contains `.git`: a
// repository's subdirectories are not separate projects.
func ScanRepositories(ctx context.Context, opts ScanOptions) ([]Candidate, error) {
	roots := opts.Roots
	if roots == nil {
		roots = DefaultScanRoots()
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 6
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
	}
	if workers < 1 {
		workers = 1
	}

	s := &scan{
		ctx:      ctx,
		maxDepth: maxDepth,
		visited:  make(map[string]bool),
		found:    make(map[string]string),
	}
	q := newDirQueue()

	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		q.push([]walkItem{{dir: filepath.Clean(abs), depth: 0}})
	}

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				it, ok := q.pop()
				if !ok {
					return
				}
				children := s.process(it)
				q.done(children)
			}
		}()
	}
	wg.Wait()

	candidates := s.candidates()
	if err := ctx.Err(); err != nil {
		return candidates, err
	}
	return candidates, nil
}

// scan holds the shared, concurrency-safe state of a single walk.
type scan struct {
	ctx      context.Context
	maxDepth int

	mu      sync.Mutex
	visited map[string]bool
	found   map[string]string // repo path -> identity
}

func (s *scan) process(it walkItem) []walkItem {
	if s.ctx.Err() != nil {
		return nil
	}

	// Never revisit a directory whose real path we have already walked; this
	// also breaks symlink loops that resolve back onto an ancestor.
	real := resolveReal(it.dir)
	key := real
	if caseInsensitiveFS {
		key = strings.ToLower(real)
	}
	s.mu.Lock()
	if s.visited[key] {
		s.mu.Unlock()
		return nil
	}
	s.visited[key] = true
	s.mu.Unlock()

	// A directory containing `.git` is a repository root: record it and stop.
	if _, err := os.Lstat(filepath.Join(it.dir, ".git")); err == nil {
		if id, err := OriginFromGitDir(it.dir); err == nil && id != "" {
			s.mu.Lock()
			s.found[it.dir] = id
			s.mu.Unlock()
		}
		return nil
	}

	if it.depth >= s.maxDepth {
		return nil
	}

	entries, err := os.ReadDir(it.dir)
	if err != nil {
		return nil
	}

	var children []walkItem
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if prunedName(name) {
			continue
		}
		full := filepath.Join(it.dir, name)
		if skipAbsDir(full) || prunedPath(full) {
			continue
		}
		// Never follow symlinks or reparse points (OneDrive/Dropbox
		// placeholders): walking a placeholder would download the file.
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			continue
		}
		children = append(children, walkItem{dir: full, depth: it.depth + 1})
	}
	return children
}

// candidates returns the collected repositories sorted by path.
func (s *scan) candidates() []Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Candidate, 0, len(s.found))
	for path, id := range s.found {
		out = append(out, Candidate{Path: path, Identity: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// resolveReal returns the real (symlink-resolved, cleaned) path of dir, falling
// back to a cleaned absolute path when it cannot be resolved.
func resolveReal(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
}

type walkItem struct {
	dir   string
	depth int
}

// dirQueue is an unbounded LIFO work queue with a pending counter. Workers block
// while it is non-empty and exit once every enqueued item has been processed,
// which avoids both one-goroutine-per-directory blowup and channel deadlock when
// a worker enqueues children while every other worker is also enqueuing.
type dirQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []walkItem
	pending int
	closed  bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *dirQueue) push(items []walkItem) {
	if len(items) == 0 {
		return
	}
	q.mu.Lock()
	q.items = append(q.items, items...)
	q.pending += len(items)
	q.mu.Unlock()
	q.cond.Broadcast()
}

func (q *dirQueue) pop() (walkItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return walkItem{}, false
	}
	it := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return it, true
}

// done records that one popped item finished, enqueuing any children it found.
// Children are counted before the parent is retired so pending never hits zero
// prematurely.
func (q *dirQueue) done(children []walkItem) {
	q.mu.Lock()
	q.items = append(q.items, children...)
	q.pending += len(children)
	q.pending--
	if q.pending == 0 {
		q.closed = true
	}
	q.mu.Unlock()
	q.cond.Broadcast()
}

// prunedNames are directory basenames never worth descending into, matched
// case-insensitively. They are dependency caches, build outputs, and OS trees
// that hold no first-party repositories.
var prunedNames = map[string]bool{
	"node_modules": true, ".venv": true, "venv": true, "vendor": true,
	"target": true, "dist": true, "build": true, "out": true,
	".next": true, ".nuxt": true, ".cache": true, ".cargo": true,
	".rustup": true, ".npm": true, ".pnpm-store": true, ".gradle": true,
	".m2": true, "library": true, "appdata": true, "programdata": true,
	"program files": true, "program files (x86)": true, "windows": true,
	"$recycle.bin": true, "system volume information": true, "snap": true,
	".steam": true, ".wine": true, "trash": true, ".trash": true,
}

func prunedName(name string) bool {
	return prunedNames[strings.ToLower(name)]
}

// prunedPath prunes by path suffix rather than basename: `.local/share` holds
// application data, not projects, but "share" alone is too common to prune.
func prunedPath(full string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.ToSlash(full)), "/.local/share")
}

// skipAbsDirs are virtual/pseudo filesystems that must never be walked.
var skipAbsDirs = map[string]bool{
	"/proc": true, "/sys": true, "/dev": true, "/run": true,
}

func skipAbsDir(full string) bool {
	return skipAbsDirs[filepath.Clean(full)]
}
