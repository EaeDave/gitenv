package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func focusReloadMsg() reloadMsg {
	return reloadMsg{
		manifest: vault.Manifest{Projects: map[string]vault.Project{
			"api": {Profiles: map[string]vault.Profile{"dev": {}, "prod": {}}},
		}},
		statuses: map[string]string{"api": "clean"},
		current:  app.CurrentProject{LinkedName: "api", Path: "/api"},
	}
}

func TestReloadLandsOnCurrentProjectWhenLaunchedInsideIt(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{
		"api":   {Path: "/api", ActiveProfile: "dev"},
		"other": {Path: "/other", ActiveProfile: "dev"},
	}}
	m := model{cfg: &cfg, screen: screenProjects}

	next, _ := m.Update(focusReloadMsg())
	got := next.(model)
	if got.screen != screenProfiles || got.selectedProject != "api" {
		t.Fatalf("did not focus current project: screen=%v selected=%q", got.screen, got.selectedProject)
	}
	if !got.landed {
		t.Fatal("landing flag was not set")
	}
	if len(got.profiles) != 2 {
		t.Fatalf("focused project profiles = %#v", got.profiles)
	}
}

func TestReloadKeepsProjectListWhenLaunchedOutsideAnyProject(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{
		"api": {Path: "/api", ActiveProfile: "dev"},
	}}
	m := model{cfg: &cfg, screen: screenProjects}

	msg := focusReloadMsg()
	msg.current = app.CurrentProject{Path: "/somewhere-else"}
	next, _ := m.Update(msg)
	got := next.(model)
	if got.screen != screenProjects || got.selectedProject != "" {
		t.Fatalf("unlinked launch should keep the project list: screen=%v selected=%q", got.screen, got.selectedProject)
	}
}

func TestReloadDoesNotRefocusAfterUnlockingBrowse(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{
		"api": {Path: "/api", ActiveProfile: "dev"},
	}}
	m := model{cfg: &cfg, screen: screenProjects, browseProjects: true}

	next, _ := m.Update(focusReloadMsg())
	got := next.(model)
	if got.screen != screenProjects {
		t.Fatalf("browse mode should not be dragged back into a single project: screen=%v", got.screen)
	}

	// A later reload must not re-focus once the initial landing happened.
	got.screen = screenProjects
	again, _ := got.Update(focusReloadMsg())
	if again.(model).screen != screenProjects {
		t.Fatalf("second reload re-focused after landing: screen=%v", again.(model).screen)
	}
}

func TestProfilesBrowseShortcutUnlocksProjectList(t *testing.T) {
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{"api": {Path: "/api", ActiveProfile: "dev"}}}
	m := model{
		cfg:             &cfg,
		screen:          screenProfiles,
		selectedProject: "api",
		current:         app.CurrentProject{LinkedName: "api"},
	}
	if !m.isFocusedProject() {
		t.Fatal("model launched inside a project should be focused")
	}
	next, _ := m.profilesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got := next.(model)
	if !got.browseProjects || got.screen != screenProjects || got.selectedProject != "" {
		t.Fatalf("p did not unlock project browsing: %#v", got)
	}
	if got.isFocusedProject() {
		t.Fatal("browsing should no longer be focused after unlocking")
	}
}

func TestProfilesEscQuitsWhenFocusedAndReturnsWhenBrowsing(t *testing.T) {
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{"api": {Path: "/api", ActiveProfile: "dev"}}}
	focused := model{cfg: &cfg, screen: screenProfiles, selectedProject: "api", current: app.CurrentProject{LinkedName: "api"}}
	_, cmd := focused.profilesKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc in a focused project should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("esc in a focused project did not emit quit")
	}

	browsing := model{cfg: &cfg, screen: screenProfiles, selectedProject: "api", browseProjects: true, current: app.CurrentProject{LinkedName: "api"}}
	next, cmd := browsing.profilesKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc while browsing should not quit")
	}
	if got := next.(model); got.screen != screenProjects || got.selectedProject != "" {
		t.Fatalf("esc while browsing should return to the project list: %#v", got)
	}
}

func TestReloadFocusesAfterMigrationCompletes(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{
		"api": {Path: "/api", ActiveProfile: "dev"},
	}}
	m := model{cfg: &cfg, screen: screenProfiles, selectedProject: "api", current: app.CurrentProject{LinkedName: "api"}}

	// A gated first load (migration required) must not consume the landing.
	gated := focusReloadMsg()
	gated.needsMigration = true
	next, _ := m.Update(gated)
	got := next.(model)
	if got.screen != screenMigrate || got.landed {
		t.Fatalf("migration gate should defer landing: screen=%v landed=%v", got.screen, got.landed)
	}

	// Once the vault is ready, the next reload focuses the current project.
	ready, _ := got.Update(focusReloadMsg())
	if focused := ready.(model); focused.screen != screenProfiles || focused.selectedProject != "api" || !focused.landed {
		t.Fatalf("focus was not restored after migration: %#v", focused)
	}
}

func TestProfilesReloadRechecksStatus(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{"api": {Path: "/api", ActiveProfile: "dev"}}}
	m := model{
		cfg:             &cfg,
		screen:          screenProfiles,
		selectedProject: "api",
		current:         app.CurrentProject{LinkedName: "api"},
		syncStatus:      gitops.SyncStatus{State: gitops.SyncSynced},
	}
	next, cmd := m.profilesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	got := next.(model)
	if cmd == nil {
		t.Fatal("reload should dispatch a refresh command")
	}
	if got.syncStatus.State != gitops.SyncChecking {
		t.Fatalf("reload should re-enter checking state: %v", got.syncStatus.State)
	}
	if got.screen != screenProfiles {
		t.Fatalf("reload should stay on the profiles screen: %v", got.screen)
	}
}
