package git

import (
	"path/filepath"
	"testing"
)

func TestAddRemoteAndReadURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	url := "git@example.test:owner/vault.git"
	if err := AddRemote(root, "origin", url); err != nil {
		t.Fatal(err)
	}
	got, err := RemoteURL(root, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if got != url {
		t.Fatalf("RemoteURL() = %q, want %q", got, url)
	}
	if !HasRemote(root, "origin") {
		t.Fatal("HasRemote returned false")
	}
}
