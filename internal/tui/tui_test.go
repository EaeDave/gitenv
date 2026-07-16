package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

func TestOnboardingAndContextualAddFlow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	project := filepath.Join(root, "my-api")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	m := newModel(&cfg, project)
	if m.screen != screenOnboarding || !m.current.HasEnv {
		t.Fatalf("unexpected initial state: screen=%v current=%#v", m.screen, m.current)
	}
	next, _ := m.onboardingKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.screen != screenCreate || len(m.fields) != 3 {
		t.Fatalf("create wizard not opened: %#v", m)
	}
	m.fields[0].value = filepath.Join(root, "vault")
	m.fields[1].value = filepath.Join(root, "recovery.txt")
	_, cmd := m.submitForm()
	msg := cmd()
	if result := msg.(operationMsg); result.err != nil {
		t.Fatal(result.err)
	}
	m.cfg = &cfg
	m.screen = screenProjects
	current, err := app.DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	m.current = current
	next, _ = m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(model)
	if m.screen != screenAddProject || m.fields[0].value != "my-api" || m.fields[1].value != "dev" {
		t.Fatalf("add wizard defaults wrong: %#v", m.fields)
	}
	_, cmd = m.submitForm()
	msg = cmd()
	if result := msg.(operationMsg); result.err != nil {
		t.Fatal(result.err)
	}
	if cfg.Projects["my-api"].ActiveProfile != "dev" {
		t.Fatalf("project not captured: %#v", cfg.Projects)
	}
}

func TestAddSuggestsUnlinkedVaultProject(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{
		cfg:     &cfg,
		screen:  screenProjects,
		current: app.CurrentProject{Path: "/different-folder", Name: "different-folder", HasEnv: true},
		manifest: vault.Manifest{Projects: map[string]vault.Project{
			"api": {Profiles: map[string]vault.Profile{"prod": {}}},
		}},
	}
	next, _ := m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := next.(model)
	if got.screen != screenAddProject || got.fields[0].value != "api" || got.fields[1].value != "prod" {
		t.Fatalf("remote project suggestion = %#v", got.fields)
	}
}

func TestProjectAndProfileKeyBindings(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{"api": {Path: "/api", ActiveProfile: "dev"}}}
	m := model{cfg: &cfg, screen: screenProjects, projects: []string{"api"}, statuses: map[string]string{"api": "clean"}}
	for key, wantScreen := range map[rune]screen{'g': screenRemote, 'b': screenRecovery} {
		copy := m
		next, _ := copy.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if next.(model).screen != wantScreen {
			t.Fatalf("key %q did not open screen %v", key, wantScreen)
		}
	}
	m.screen = screenProfiles
	m.selectedProject = "api"
	m.profiles = []string{"dev"}
	next, _ := m.profilesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if next.(model).screen != screenNewProfile {
		t.Fatal("n did not open new profile form")
	}
	m.profiles = []string{"prod"}
	next, _ = m.profilesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(model)
	if got.screen != screenConfirmDelete || got.pendingProfile != "prod" {
		t.Fatalf("d did not request profile removal: %#v", got)
	}
	m.profiles = []string{"dev"}
	next, _ = m.profilesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got = next.(model)
	if got.screen != screenProfiles || got.errText == "" {
		t.Fatalf("active profile removal was not blocked: %#v", got)
	}
}

func TestReloadClampsProfileCursorAfterRemoval(t *testing.T) {
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{"api": {}}}
	m := model{cfg: &cfg, selectedProject: "api", profiles: []string{"dev", "prod"}, profileCursor: 1}
	manifest := vault.Manifest{Projects: map[string]vault.Project{"api": {Profiles: map[string]vault.Profile{"dev": {}}}}}
	next, _ := m.Update(reloadMsg{manifest: manifest, statuses: map[string]string{}})
	got := next.(model)
	if got.profileCursor != 0 || len(got.profiles) != 1 {
		t.Fatalf("profile cursor not clamped: %#v", got)
	}
}
