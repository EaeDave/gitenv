package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

// mkRepo creates a git repository root at path with the given origin url,
// writing the config by hand so the tests never invoke git.
func mkRepo(t *testing.T, path, origin string) {
	t.Helper()
	gitDir := filepath.Join(path, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", gitDir, err)
	}
	writeConfig(t, filepath.Join(gitDir, "config"), origin)
}

func writeConfig(t *testing.T, path, origin string) {
	t.Helper()
	content := "[core]\n\trepositoryformatversion = 0\n" +
		"[remote \"origin\"]\n\turl = " + origin + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
}

func TestOriginFromGitDir(t *testing.T) {
	t.Run("https origin normalizes to canonical identity", func(t *testing.T) {
		root := t.TempDir()
		mkRepo(t, root, "https://GitHub.com/owner/repo.git")
		got, err := OriginFromGitDir(root)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "github.com/owner/repo"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("scp-like origin", func(t *testing.T) {
		root := t.TempDir()
		mkRepo(t, root, "git@github.com:owner/repo.git")
		got, err := OriginFromGitDir(root)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "github.com/owner/repo"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("url without spaces around equals", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[remote \"origin\"]\n\turl=https://gitlab.com/team/proj.git\n"
		if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := OriginFromGitDir(root)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "gitlab.com/team/proj"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("ignores other remotes and stops at next section", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[remote \"upstream\"]\n\turl = https://github.com/other/fork.git\n" +
			"[remote \"origin\"]\n\turl = https://github.com/owner/repo.git\n" +
			"[branch \"main\"]\n\turl = https://wrong.example/x.git\n"
		if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := OriginFromGitDir(root)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "github.com/owner/repo"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("git file points at a gitdir elsewhere", func(t *testing.T) {
		base := t.TempDir()
		repoRoot := filepath.Join(base, "worktree")
		if err := os.MkdirAll(repoRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		realGitDir := filepath.Join(base, "real-git-dir")
		if err := os.MkdirAll(realGitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, filepath.Join(realGitDir, "config"), "https://github.com/owner/repo.git")

		// Relative gitdir must resolve against repoRoot.
		rel, err := filepath.Rel(repoRoot, realGitDir)
		if err != nil {
			t.Fatal(err)
		}
		gitFile := filepath.Join(repoRoot, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := OriginFromGitDir(repoRoot)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "github.com/owner/repo"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("linked worktree follows commondir", func(t *testing.T) {
		base := t.TempDir()
		// The shared repository holds the remotes.
		commonGit := filepath.Join(base, "main", ".git")
		if err := os.MkdirAll(commonGit, 0o755); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, filepath.Join(commonGit, "config"), "https://github.com/owner/repo.git")

		// The worktree's gitdir has no config, only a commondir pointer.
		wtGitDir := filepath.Join(commonGit, "worktrees", "wt")
		if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		wtRoot := filepath.Join(base, "wt")
		if err := os.MkdirAll(wtRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wtRoot, ".git"), []byte("gitdir: "+wtGitDir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := OriginFromGitDir(wtRoot)
		if err != nil {
			t.Fatalf("OriginFromGitDir: %v", err)
		}
		if want := "github.com/owner/repo"; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	})

	t.Run("no origin is an error", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[remote \"upstream\"]\n\turl = https://github.com/other/fork.git\n"
		if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := OriginFromGitDir(root); err == nil {
			t.Fatal("expected error for missing origin")
		}
	})

	t.Run("no .git is an error", func(t *testing.T) {
		if _, err := OriginFromGitDir(t.TempDir()); err == nil {
			t.Fatal("expected error for missing .git")
		}
	})
}

func paths(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Path
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestScanRepositories(t *testing.T) {
	t.Run("finds a repo with its canonical identity", func(t *testing.T) {
		root := t.TempDir()
		repo := filepath.Join(root, "myproj")
		mkRepo(t, repo, "https://github.com/owner/repo.git")

		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
		}
		if got[0].Path != repo {
			t.Fatalf("path = %q, want %q", got[0].Path, repo)
		}
		if want := "github.com/owner/repo"; got[0].Identity != want {
			t.Fatalf("identity = %q, want %q", got[0].Identity, want)
		}
	})

	t.Run("does not walk into a nested repo", func(t *testing.T) {
		root := t.TempDir()
		outer := filepath.Join(root, "outer")
		mkRepo(t, outer, "https://github.com/owner/outer.git")
		inner := filepath.Join(outer, "inner")
		mkRepo(t, inner, "https://github.com/owner/inner.git")

		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		p := paths(got)
		if !contains(p, outer) {
			t.Fatalf("outer repo missing: %v", p)
		}
		if contains(p, inner) {
			t.Fatalf("inner repo should not be walked into: %v", p)
		}
	})

	t.Run("prunes node_modules", func(t *testing.T) {
		root := t.TempDir()
		good := filepath.Join(root, "app")
		mkRepo(t, good, "https://github.com/owner/app.git")
		buried := filepath.Join(root, "app-host", "node_modules", "dep")
		mkRepo(t, buried, "https://github.com/owner/dep.git")

		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		p := paths(got)
		if !contains(p, good) {
			t.Fatalf("app repo missing: %v", p)
		}
		if contains(p, buried) {
			t.Fatalf("node_modules repo should be pruned: %v", p)
		}
	})

	t.Run("symlink loop does not hang", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation is unreliable on windows")
		}
		root := t.TempDir()
		repo := filepath.Join(root, "proj")
		mkRepo(t, repo, "https://github.com/owner/proj.git")

		// A directory that symlinks back onto its own parent: a walk that
		// followed it would loop forever.
		deep := filepath.Join(root, "deep")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(root, filepath.Join(deep, "loop")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		done := make(chan []Candidate, 1)
		go func() {
			c, _ := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
			done <- c
		}()
		select {
		case got := <-done:
			if !contains(paths(got), repo) {
				t.Fatalf("proj repo missing: %v", paths(got))
			}
		case <-time.After(10 * time.Second):
			t.Fatal("scan hung on a symlink loop")
		}
	})

	t.Run("respects MaxDepth", func(t *testing.T) {
		root := t.TempDir()
		shallow := filepath.Join(root, "shallow")
		mkRepo(t, shallow, "https://github.com/owner/shallow.git")
		deep := filepath.Join(root, "a", "b", "c", "deep")
		mkRepo(t, deep, "https://github.com/owner/deep.git")

		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}, MaxDepth: 2})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		p := paths(got)
		if !contains(p, shallow) {
			t.Fatalf("shallow repo missing at depth 1: %v", p)
		}
		if contains(p, deep) {
			t.Fatalf("deep repo should be beyond MaxDepth: %v", p)
		}
	})

	t.Run("git file candidate yields origin", func(t *testing.T) {
		root := t.TempDir()
		base := filepath.Join(root, "wt")
		repoRoot := filepath.Join(base, "checkout")
		if err := os.MkdirAll(repoRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		realGitDir := filepath.Join(base, "gitdir")
		if err := os.MkdirAll(realGitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, filepath.Join(realGitDir, "config"), "https://github.com/owner/wt.git")
		if err := os.WriteFile(filepath.Join(repoRoot, ".git"), []byte("gitdir: "+realGitDir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		var found *Candidate
		for i := range got {
			if got[i].Path == repoRoot {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatalf("checkout repo missing: %v", paths(got))
		}
		if want := "github.com/owner/wt"; found.Identity != want {
			t.Fatalf("identity = %q, want %q", found.Identity, want)
		}
	})

	t.Run("cancelled context returns ctx.Err quickly", func(t *testing.T) {
		root := t.TempDir()
		// A broad tree so an un-cancelled scan would take real time.
		for i := range 20 {
			mkRepo(t, filepath.Join(root, "grp", string(rune('a'+i)), "proj"), "https://github.com/owner/x.git")
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		type res struct {
			cands []Candidate
			err   error
		}
		done := make(chan res, 1)
		go func() {
			c, err := ScanRepositories(ctx, ScanOptions{Roots: []string{root}})
			done <- res{c, err}
		}()
		select {
		case r := <-done:
			if r.err != context.Canceled {
				t.Fatalf("err = %v, want context.Canceled", r.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled scan did not return promptly")
		}
	})

	t.Run("results are sorted by path", func(t *testing.T) {
		root := t.TempDir()
		for _, n := range []string{"c", "a", "b"} {
			mkRepo(t, filepath.Join(root, n), "https://github.com/owner/"+n+".git")
		}
		got, err := ScanRepositories(context.Background(), ScanOptions{Roots: []string{root}})
		if err != nil {
			t.Fatalf("ScanRepositories: %v", err)
		}
		p := paths(got)
		if !sort.StringsAreSorted(p) {
			t.Fatalf("candidates not sorted by path: %v", p)
		}
	})
}

func TestDefaultScanRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)              // unix
	t.Setenv("USERPROFILE", home)       // windows
	t.Setenv("GITENV_CONFIG_DIR", home) // keep config resolution inside the sandbox

	// Create a couple of the well-known directories; leave others absent.
	dev := filepath.Join(home, "dev")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Documents", "GitHub"), 0o755); err != nil {
		t.Fatal(err)
	}

	roots := DefaultScanRoots()

	// Every returned root must be an existing, absolute directory.
	seen := make(map[string]bool)
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			t.Fatalf("root not absolute: %q", r)
		}
		if seen[r] {
			t.Fatalf("duplicate root: %q", r)
		}
		seen[r] = true
		info, err := os.Stat(r)
		if err != nil || !info.IsDir() {
			t.Fatalf("root is not an existing dir: %q", r)
		}
		if r == string(filepath.Separator) {
			t.Fatalf("filesystem root must never be a scan root: %q", r)
		}
	}

	if !contains(roots, dev) {
		t.Fatalf("expected %q among roots: %v", dev, roots)
	}
	// Home itself is the deep fallback and must come last.
	if len(roots) == 0 || roots[len(roots)-1] != home {
		t.Fatalf("home should be the last root; got %v", roots)
	}
}
