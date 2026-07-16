package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
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
	if m.screen != screenCreate || len(m.fields) != 5 {
		t.Fatalf("create wizard not opened: screen=%v fields=%d", m.screen, len(m.fields))
	}
	m.fields[0].value = filepath.Join(root, "vault")
	m.fields[1].value = "testpassword"
	m.fields[2].value = "testpassword"
	// fields[3] = device name (empty OK), fields[4] = remote (empty OK)
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

func TestAddDoesNotSuggestUnrelatedVaultProject(t *testing.T) {
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
	if got.screen != screenAddProject || got.fields[0].value != "different-folder" || got.fields[1].value != "dev" {
		t.Fatalf("unrelated vault project leaked into defaults: %#v", got.fields)
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

// TestRemoteMenuFlow verifies the full remote cursor-menu UX.
func TestRemoteMenuFlow(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects, projects: []string{}}

	// g always opens the remote menu regardless of whether a remote exists.
	next, _ := m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = next.(model)
	if m.screen != screenRemote {
		t.Fatalf("g did not open remote menu; got screen %v", m.screen)
	}
	if m.menuCursor != 0 {
		t.Fatalf("remote menu cursor not reset to 0")
	}

	// Change (cursor 0) with a cached prefill URL opens screenRemoteChange prefilled.
	m.remoteURL = "https://example.com/vault.git"
	m.menuCursor = 0
	next, _ = m.remoteMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.screen != screenRemoteChange {
		t.Fatalf("Change did not open screenRemoteChange; got %v", m.screen)
	}
	if len(m.fields) != 1 || m.fields[0].value != "https://example.com/vault.git" {
		t.Fatalf("Change form not prefilled: %#v", m.fields)
	}

	// Esc from the Change form returns to the remote menu (not projects).
	next, _ = m.formKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.screen != screenRemote {
		t.Fatalf("esc from Change form did not return to remote menu; got %v", m.screen)
	}

	// Remove (cursor 2) opens the confirmation screen.
	m.menuCursor = 2
	next, _ = m.remoteMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.screen != screenConfirmRemoveRemote {
		t.Fatalf("Remove did not open screenConfirmRemoveRemote; got %v", m.screen)
	}

	// Back (cursor 3) returns to projects.
	m2 := model{cfg: &cfg, screen: screenRemote, menuCursor: 3}
	next, _ = m2.remoteMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(model).screen != screenProjects {
		t.Fatalf("Back did not return to projects; got %v", next.(model).screen)
	}

	// Test (cursor 1) stays on remote menu (screen unchanged after opCmd result).
	m3 := model{cfg: &cfg, screen: screenRemote, menuCursor: 1}
	next, _ = m3.remoteMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if !got.busy {
		t.Fatalf("Test did not set busy flag")
	}
	// Screen stays on screenRemote — operationMsg with err nil leaves it there.
	got.busy = false
	got2, _ := got.Update(operationMsg{info: "reachable"})
	if got2.(model).screen != screenRemote {
		t.Fatalf("after successful Test op, expected screenRemote; got %v", got2.(model).screen)
	}
}

// TestRemoteRemoveConfirmation verifies y/N behaviour of the remove confirmation.
func TestRemoteRemoveConfirmation(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{cfg: &cfg, screen: screenConfirmRemoveRemote}

	// N (or any non-y) cancels and returns to the remote menu.
	next, _ := m.confirmRemoveRemoteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got := next.(model)
	if got.screen != screenRemote {
		t.Fatalf("n did not return to remote menu; got %v", got.screen)
	}
	if got.info != "cancelled" {
		t.Fatalf("n did not set cancelled info: %q", got.info)
	}

	// y sets busy and returns an opCmd (real git op, not executed in this test).
	next, _ = m.confirmRemoveRemoteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got = next.(model)
	if !got.busy {
		t.Fatalf("y did not set busy for Remove")
	}
}

func TestAddSuggestsSameBasenameVaultProject(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{
		cfg:     &cfg,
		screen:  screenProjects,
		current: app.CurrentProject{Path: "/workspace/api", Name: "api", HasEnv: true},
		manifest: vault.Manifest{Projects: map[string]vault.Project{
			"api": {Profiles: map[string]vault.Profile{"prod": {}}},
		}},
	}
	next, _ := m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := next.(model)
	if got.fields[0].value != "api" || got.fields[1].value != "prod" {
		t.Fatalf("same-basename project was not suggested: %#v", got.fields)
	}
}

func TestLoadAutoLinksExactRemoteWithoutApplyingProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	vault.SetSessionIdentity(identity)
	t.Cleanup(vault.ClearSessionIdentity)
	vaultPath := filepath.Join(root, "vault")
	if err := vault.Init(vaultPath, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Projects["api"] = vault.Project{Repositories: []string{"github.com/acme/api"}, Profiles: map[string]vault.Profile{"dev": {}}}
	if err := vault.SaveManifest(vaultPath, manifest); err != nil {
		t.Fatal(err)
	}
	if err := vault.ProtectVault(vaultPath, "correct horse battery staple", "test-device", vault.StoreIdentitySession); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "renamed-api")
	if err := gitops.Init(project); err != nil {
		t.Fatal(err)
	}
	if err := gitops.AddRemote(project, "origin", "git@github.com:acme/api.git"); err != nil {
		t.Fatal(err)
	}
	original := []byte("LOCAL=preserve\n")
	if err := os.WriteFile(filepath.Join(project, ".env"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{VaultPath: vaultPath, Projects: map[string]vault.LocalProject{}}
	msg := loadCmd(&cfg, project)()
	reload, ok := msg.(reloadMsg)
	if !ok || reload.current.LinkedName != "api" || cfg.Projects["api"].Path != project {
		t.Fatalf("exact remote was not auto-linked: msg=%#v cfg=%#v", msg, cfg)
	}
	after, err := os.ReadFile(filepath.Join(project, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("auto-link overwrote local .env: %q", after)
	}
}

// TestMigrationRouting verifies that reloadMsg with needsMigration routes to
// the migration form and populates the expected fields.
func TestMigrationRouting(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects}

	next, _ := m.Update(reloadMsg{
		manifest:       vault.Manifest{},
		statuses:       map[string]string{},
		needsMigration: true,
	})
	got := next.(model)
	if got.screen != screenMigrate {
		t.Fatalf("needsMigration did not route to screenMigrate; got %v", got.screen)
	}
	if len(got.fields) != 3 {
		t.Fatalf("migration form should have 3 fields; got %d", len(got.fields))
	}
	if !got.fields[0].masked || !got.fields[1].masked {
		t.Fatal("migration password fields are not masked")
	}
	if got.fields[2].masked {
		t.Fatal("migration device name field should not be masked")
	}
}

// TestUnlockMenuRouting verifies that reloadMsg with needsUnlock routes to the
// unlock menu and that the menu offers password / enrollment / import-recovery.
func TestUnlockMenuRouting(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects}

	next, _ := m.Update(reloadMsg{
		manifest:    vault.Manifest{},
		statuses:    map[string]string{},
		needsUnlock: true,
	})
	got := next.(model)
	if got.screen != screenUnlock {
		t.Fatalf("needsUnlock did not route to screenUnlock; got %v", got.screen)
	}

	// Option 0 → screenUnlockPassword with a single masked field.
	got.menuCursor = 0
	next, _ = got.unlockMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	pw := next.(model)
	if pw.screen != screenUnlockPassword {
		t.Fatalf("option 0 did not open screenUnlockPassword; got %v", pw.screen)
	}
	if len(pw.fields) != 1 || !pw.fields[0].masked {
		t.Fatalf("unlock password field not masked: %#v", pw.fields)
	}
	// Esc returns to unlock menu.
	back, _ := pw.formKey(tea.KeyMsg{Type: tea.KeyEsc})
	if back.(model).screen != screenUnlock {
		t.Fatalf("esc from unlock password did not return to screenUnlock")
	}

	// Option 1 (no pending) → screenEnrollRequest.
	got.menuCursor = 1
	next, _ = got.unlockMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	enroll := next.(model)
	if enroll.screen != screenEnrollRequest {
		t.Fatalf("option 1 (no pending) did not open screenEnrollRequest; got %v", enroll.screen)
	}

	// Option 1 with pending enrollment → sets busy (ActivateDeviceEnrollment).
	got.cfg.PendingEnrollmentID = "req-abc123"
	got.menuCursor = 1
	next, _ = got.unlockMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	activate := next.(model)
	if !activate.busy {
		t.Fatalf("option 1 (pending) did not set busy for ActivateDeviceEnrollment")
	}

	// Option 2 → screenImportRecovery.
	got.cfg.PendingEnrollmentID = ""
	got.menuCursor = 2
	next, _ = got.unlockMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	imp := next.(model)
	if imp.screen != screenImportRecovery {
		t.Fatalf("option 2 did not open screenImportRecovery; got %v", imp.screen)
	}
	if len(imp.fields) != 1 || imp.fields[0].label != "Recovery key" || !imp.fields[0].masked {
		t.Fatalf("recovery paste field is not masked: %#v", imp.fields)
	}

	// Option 3 → explicit local disconnect confirmation.
	got.menuCursor = 3
	next, _ = got.unlockMenuKey(tea.KeyMsg{Type: tea.KeyEnter})
	if disconnect := next.(model); disconnect.screen != screenConfirmDisconnect {
		t.Fatalf("option 3 did not open disconnect confirmation; got %v", disconnect.screen)
	}
}

func TestAccessGateCannotBeBypassed(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune{'q'}}} {
		m := model{cfg: &cfg, screen: screenUnlock, accessRequired: true}
		next, cmd := m.unlockMenuKey(key)
		got := next.(model)
		if cmd != nil || got.screen != screenUnlock {
			t.Fatalf("locked %q bypassed access gate: screen=%v cmd=%v", key.String(), got.screen, cmd)
		}
	}
	m := model{cfg: &cfg, screen: screenMigrate, accessRequired: true, fields: []field{{"Master password", "secret", true}}}
	next, cmd := m.formKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); cmd != nil || got.screen != screenMigrate {
		t.Fatalf("migration escape bypassed access gate: screen=%v cmd=%v", got.screen, cmd)
	}
	m = model{cfg: &cfg, screen: screenImportRecovery, accessRequired: true, migrationRecoveryRequired: true, fields: []field{{"Recovery key", "", true}}}
	next, cmd = m.formKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); cmd != nil || got.screen != screenUnlock {
		t.Fatalf("recovery escape did not return to locked menu: screen=%v cmd=%v", got.screen, cmd)
	}
}

func TestDisconnectConfirmationReturnsToOnboarding(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	cfg := vault.LocalConfig{VaultPath: filepath.Join(root, "vault"), Projects: map[string]vault.LocalProject{"api": {Path: root}}}
	m := model{cfg: &cfg, screen: screenConfirmDisconnect, accessRequired: true}
	next, cmd := m.confirmDisconnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := next.(model)
	if got.screen != screenOnboarding || !got.busy || cmd == nil {
		t.Fatalf("disconnect confirmation did not enter onboarding: %#v", got)
	}
	result := cmd().(operationMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if cfg.VaultPath != "" || len(cfg.Projects) != 0 {
		t.Fatalf("disconnect did not clear local config: %#v", cfg)
	}
}

func TestMigrationRejectsUnauthorizedLoadedIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	allowed, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	vaultPath := filepath.Join(root, "vault")
	if err := vault.Init(vaultPath, allowed.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	stale, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	vault.SetSessionIdentity(stale)
	t.Cleanup(vault.ClearSessionIdentity)
	cfg := vault.LocalConfig{VaultPath: vaultPath, Projects: map[string]vault.LocalProject{}}
	msg := loadCmd(&cfg, root)()
	reload, ok := msg.(reloadMsg)
	if !ok || !reload.migrationIdentityMissing || reload.needsMigration {
		t.Fatalf("unauthorized identity passed migration gate: %#v", msg)
	}
}

func TestPasswordFieldsRenderMasked(t *testing.T) {
	cfg := vault.LocalConfig{}
	m := model{cfg: &cfg, screen: screenUnlockPassword, fields: []field{{"Master password", "sëcret", true}}}
	view := m.View()
	if strings.Contains(view, "sëcret") || !strings.Contains(view, "******") {
		t.Fatalf("password was not masked: %q", view)
	}
}

func TestWindowSizeUpdatesResponsiveDimensions(t *testing.T) {
	cfg := vault.LocalConfig{}
	m := model{cfg: &cfg}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 108, Height: 32})
	got := next.(model)
	if got.width != 108 || got.height != 32 {
		t.Fatalf("window size not retained: width=%d height=%d", got.width, got.height)
	}
}

func TestProjectViewAdaptsBetweenWideAndCompactLayouts(t *testing.T) {
	cfg := vault.LocalConfig{
		VaultPath: "/vault",
		Projects: map[string]vault.LocalProject{
			"api": {Path: "/workspace/api", ActiveProfile: "dev"},
		},
	}
	m := model{
		cfg:      &cfg,
		screen:   screenProjects,
		projects: []string{"api"},
		statuses: map[string]string{"api": "clean"},
		current:  app.CurrentProject{Path: "/workspace/api", HasEnv: true, LinkedName: "api"},
	}

	m.width = 108
	wide := m.View()
	m.width = 60
	compact := m.View()
	wideProjectsLine := lineContaining(t, wide, "Projects")
	wideWorkspaceLine := lineContaining(t, wide, "Workspace")
	compactProjectsLine := lineContaining(t, compact, "Projects")
	compactWorkspaceLine := lineContaining(t, compact, "Workspace")
	if wideProjectsLine != wideWorkspaceLine {
		t.Fatalf("wide layout did not place panels side by side:\n%s", wide)
	}
	if compactProjectsLine == compactWorkspaceLine {
		t.Fatalf("compact layout did not stack panels:\n%s", compact)
	}
	for _, text := range []string{"api", "dev", "clean", ".env found", "linked: api"} {
		if !strings.Contains(wide, text) || !strings.Contains(compact, text) {
			t.Fatalf("responsive view lost %q", text)
		}
	}
}

func TestStatusRenderingIncludesTextWithoutColor(t *testing.T) {
	for _, status := range []string{"clean", "modified", "error", "unknown"} {
		if rendered := renderStatus(status); !strings.Contains(rendered, status) {
			t.Fatalf("status %q relies on color alone: %q", status, rendered)
		}
	}
}

func lineContaining(t *testing.T, text, fragment string) int {
	t.Helper()
	for index, line := range strings.Split(text, "\n") {
		if strings.Contains(line, fragment) {
			return index
		}
	}
	t.Fatalf("view does not contain %q:\n%s", fragment, text)
	return -1
}
