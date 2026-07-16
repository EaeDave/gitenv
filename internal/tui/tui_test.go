package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/envdiff"
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
	m := newModel(&cfg, project, "dev")
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
	next, cmd = m.submitForm()
	preview := cmd().(capturePreviewMsg)
	if preview.err != nil {
		t.Fatal(preview.err)
	}
	next, _ = next.(model).Update(preview)
	m = next.(model)
	if m.screen != screenConfirmCapture {
		t.Fatalf("capture preview not opened: screen=%v", m.screen)
	}
	next, cmd = m.confirmCaptureKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	msg = cmd()
	if result := msg.(operationMsg); result.err != nil {
		t.Fatal(result.err)
	}
	m = next.(model)
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

func TestSyncStatusMessageUpdatesWithoutBlockingUI(t *testing.T) {
	cfg := vault.LocalConfig{}
	m := model{cfg: &cfg, screen: screenProjects, syncStatus: gitops.SyncStatus{State: gitops.SyncChecking}}
	checked := gitops.SyncStatus{State: gitops.SyncRemoteAhead, Behind: 2}
	next, cmd := m.Update(syncStatusMsg{status: checked})
	got := next.(model)
	if cmd != nil || got.busy || got.screen != screenProjects || got.syncStatus != checked {
		t.Fatalf("async sync update disrupted UI: %#v cmd=%v", got, cmd)
	}
}

func TestSyncPanelSeparatesEnvironmentAndRemoteState(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{"api": {Path: "/api", ActiveProfile: "dev"}}}
	m := model{
		cfg:              &cfg,
		screen:           screenProjects,
		projects:         []string{"api"},
		statuses:         map[string]string{"api": "clean"},
		remoteDisplayURL: "github.com/example/vault",
		syncStatus:       gitops.SyncStatus{State: gitops.SyncRemoteAhead, Behind: 2},
	}
	view := m.View()
	for _, text := range []string{"clean", "Sync", "↓ 2 remote update(s)", "Press s to download", "Local .env files"} {
		if text == "Local .env files" {
			continue
		}
		if !strings.Contains(view, text) {
			t.Fatalf("sync panel missing %q:\n%s", text, view)
		}
	}
}

func TestContextualSyncRequiresConfirmation(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	for _, tc := range []struct {
		state gitops.SyncState
		want  string
	}{
		{gitops.SyncRemoteAhead, "Download remote vault updates?"},
		{gitops.SyncLocalAhead, "Publish local vault changes?"},
	} {
		m := model{cfg: &cfg, screen: screenProjects, syncStatus: gitops.SyncStatus{State: tc.state}}
		next, cmd := m.requestContextualSync()
		got := next.(model)
		if cmd != nil || got.screen != screenConfirmSync || got.pendingSync != tc.state {
			t.Fatalf("state %q did not request confirmation: %#v", tc.state, got)
		}
		view := got.View()
		if !strings.Contains(view, tc.want) || !strings.Contains(view, "Local .env files will not be modified") {
			t.Fatalf("confirmation for %q is unclear:\n%s", tc.state, view)
		}
		cancelled, cancelCmd := got.confirmSyncKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		if cancelCmd != nil || cancelled.(model).screen != screenProjects {
			t.Fatalf("sync cancellation failed: %#v", cancelled)
		}
	}
}

func TestContextualSyncBlocksUnsafeStates(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	for _, state := range []gitops.SyncState{gitops.SyncDiverged, gitops.SyncOffline, gitops.SyncAuthError, gitops.SyncNoRemote} {
		m := model{cfg: &cfg, screen: screenProjects, syncStatus: gitops.SyncStatus{State: state}}
		next, cmd := m.requestContextualSync()
		got := next.(model)
		if cmd != nil || got.screen != screenProjects || got.errText == "" {
			t.Fatalf("unsafe state %q was not blocked: %#v cmd=%v", state, got, cmd)
		}
	}
}

func TestNewProfileRequiresPreviewAndCancellationCreatesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	vaultDir, projectDir := filepath.Join(root, "vault"), filepath.Join(root, "project")
	if err := vault.Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("TOKEN=secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{VaultPath: vaultDir, Projects: map[string]vault.LocalProject{}}
	if err := vault.Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	m := model{cfg: &cfg, manifest: manifest, screen: screenNewProfile, selectedProject: "api", fields: []field{{"New profile name", "prod", false}}}
	next, cmd := m.submitForm()
	preview := cmd().(capturePreviewMsg)
	if preview.err != nil {
		t.Fatal(preview.err)
	}
	next, _ = next.(model).Update(preview)
	m = next.(model)
	if m.screen != screenConfirmCapture || strings.Contains(m.View(), "secret-value") {
		t.Fatalf("new profile preview unsafe or missing:\n%s", m.View())
	}
	cancelled, cancelCmd := m.confirmCaptureKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cancelCmd != nil || cancelled.(model).screen != screenProfiles {
		t.Fatalf("new profile cancellation failed: %#v", cancelled)
	}
	manifest, err = vault.LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest.Projects["api"].Profiles["prod"]; exists {
		t.Fatal("cancelled new profile preview created a profile")
	}
}

func TestCapturePreviewHidesValuesAndWritesOnlyAfterConfirmation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	vaultDir, projectDir := filepath.Join(root, "vault"), filepath.Join(root, "project")
	if err := vault.Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := []byte("API_KEY=old-secret\n# DEBUG=true\nREMOVED=retired-token-9381\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), base, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{VaultPath: vaultDir, Projects: map[string]vault.LocalProject{}}
	if err := vault.Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	before := manifest.Projects["api"].Profiles["dev"].Checksum
	current := []byte("API_KEY=new-secret\nDEBUG=true\nADDED=private\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), current, 0o600); err != nil {
		t.Fatal(err)
	}
	m := model{cfg: &cfg, screen: screenProfiles, selectedProject: "api", manifest: manifest}
	next, cmd := m.requestCapturePreview("api", "dev", captureExistingProfile)
	if !next.(model).busy || cmd == nil {
		t.Fatal("capture preview did not start asynchronously")
	}
	preview := cmd().(capturePreviewMsg)
	if preview.err != nil {
		t.Fatal(preview.err)
	}
	updated, _ := next.(model).Update(preview)
	m = updated.(model)
	view := m.View()
	for _, key := range []string{"API_KEY", "DEBUG", "ADDED", "REMOVED", "Values are hidden"} {
		if !strings.Contains(view, key) {
			t.Fatalf("capture preview missing %q:\n%s", key, view)
		}
	}
	for _, secret := range []string{"old-secret", "new-secret", "private", "retired-token-9381"} {
		if strings.Contains(view, secret) {
			t.Fatalf("capture preview exposed secret %q:\n%s", secret, view)
		}
	}
	cancelled, cancelCmd := m.confirmCaptureKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cancelCmd != nil || cancelled.(model).screen != screenProfiles {
		t.Fatalf("capture cancellation failed: %#v", cancelled)
	}
	manifest, err = vault.LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Projects["api"].Profiles["dev"].Checksum; got != before {
		t.Fatalf("cancelled preview changed profile checksum: %q", got)
	}
	m = updated.(model)
	confirmed, captureCmd := m.confirmCaptureKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !confirmed.(model).busy || captureCmd == nil {
		t.Fatal("capture confirmation did not start capture")
	}
	result := captureCmd().(operationMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	manifest, err = vault.LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Projects["api"].Profiles["dev"].Checksum; got != vault.Checksum(current) {
		t.Fatalf("confirmed capture checksum = %q", got)
	}
}
func TestSyncPanelRendersAutomaticValueFreeInventory(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:        &cfg,
		screen:     screenProjects,
		syncStatus: gitops.SyncStatus{State: gitops.SyncLocalAhead, Ahead: 1, Dirty: true},
		syncInventory: app.SyncInventory{
			Available: true,
			Committed: vault.VaultDelta{Profiles: []vault.ProfileDelta{{
				Project: "api", Profile: "prod", Kind: vault.ProfileChanged,
				Diff: envdiff.Diff{Changes: []envdiff.Change{{Key: "DATABASE_URL", Kind: envdiff.Changed}}},
			}}},
			Uncommitted: vault.VaultDelta{Profiles: []vault.ProfileDelta{{
				Project: "worker", Profile: "dev", Kind: vault.ProfileAdded,
			}}},
		},
	}
	view := m.View()
	for _, expected := range []string{"Committed, not published", "api / prod", "DATABASE_URL", "Uncommitted vault changes", "+ worker / dev", "Values hidden"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("automatic sync diff missing %q:\n%s", expected, view)
		}
	}
	for _, secret := range []string{"postgres://production-secret", "token-value"} {
		if strings.Contains(view, secret) {
			t.Fatalf("automatic sync diff exposed %q:\n%s", secret, view)
		}
	}
}

func TestSyncPanelExplainsCommitWithoutVaultContentChanges(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:           &cfg,
		screen:        screenProjects,
		syncStatus:    gitops.SyncStatus{State: gitops.SyncLocalAhead, Ahead: 1},
		syncInventory: app.SyncInventory{Available: true},
	}
	view := m.View()
	if !strings.Contains(view, "↑ 1 commit(s), no vault content changes") {
		t.Fatalf("commit-only summary missing:\n%s", view)
	}
}

func TestSyncPanelFailsClosedWhenInventoryUnavailable(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:           &cfg,
		screen:        screenProjects,
		syncStatus:    gitops.SyncStatus{State: gitops.SyncLocalAhead, Dirty: true},
		syncInventory: app.SyncInventory{Detail: "change details unavailable"},
	}
	view := m.View()
	if !strings.Contains(view, "change details unavailable") || strings.Contains(view, "DATABASE_URL") {
		t.Fatalf("unavailable inventory did not fail closed:\n%s", view)
	}
}

func TestSyncDiffViewerOpensScrollsAndReturnsWithoutValues(t *testing.T) {
	changes := make([]envdiff.Change, 0, 14)
	for index := range 14 {
		changes = append(changes, envdiff.Change{Key: fmt.Sprintf("KEY_%02d", index), Kind: envdiff.Changed})
	}
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:        &cfg,
		screen:     screenProjects,
		width:      100,
		height:     20,
		syncStatus: gitops.SyncStatus{State: gitops.SyncLocalAhead, Ahead: 1},
		syncInventory: app.SyncInventory{
			Available: true,
			LocalEnvs: []app.LocalEnvDelta{{
				Project: "millennium-api-docs", Profile: "prod",
				Diff: envdiff.Diff{Changes: []envdiff.Change{{Key: "LOCAL_KEY", Kind: envdiff.Changed}}},
			}},
			Committed: vault.VaultDelta{Profiles: []vault.ProfileDelta{{
				Project: "api", Profile: "prod", Kind: vault.ProfileChanged, Diff: envdiff.Diff{Changes: changes},
			}}},
		},
	}

	opened, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = opened.(model)
	if cmd != nil || m.screen != screenSyncDiff || m.syncDiffOffset != 0 {
		t.Fatalf("v did not open diff viewer: %#v", m)
	}
	firstPage := m.View()
	for _, expected := range []string{"Environment changes", "Local .env changes", "millennium-api-docs / prod", "LOCAL_KEY", "Lines 1–8 of", "esc/q", "back"} {
		if !strings.Contains(firstPage, expected) {
			t.Fatalf("diff viewer missing %q:\n%s", expected, firstPage)
		}
	}
	if strings.Contains(firstPage, "KEY_13") {
		t.Fatalf("first page ignored viewport limit:\n%s", firstPage)
	}

	ended, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	m = ended.(model)
	lastPage := m.View()
	if m.syncDiffOffset != m.syncDiffMaxOffset() || !strings.Contains(lastPage, "KEY_13") || !strings.Contains(lastPage, "Values hidden") {
		t.Fatalf("end did not reveal final value-free page: offset=%d max=%d\n%s", m.syncDiffOffset, m.syncDiffMaxOffset(), lastPage)
	}
	for _, secret := range []string{"postgres://production-secret", "token-value"} {
		if strings.Contains(strings.Join(m.syncDiffLines(), "\n"), secret) {
			t.Fatalf("diff viewer exposed %q", secret)
		}
	}

	back, backCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if backCmd != nil || back.(model).screen != screenProjects || back.(model).syncDiffOffset != 0 {
		t.Fatalf("esc did not return to dashboard: %#v", back)
	}
}

func TestSyncDiffViewerClampsPagingAndShowsUnavailableState(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:            &cfg,
		screen:         screenSyncDiff,
		width:          80,
		height:         14,
		syncStatus:     gitops.SyncStatus{State: gitops.SyncLocalAhead, Dirty: true},
		syncInventory:  app.SyncInventory{Detail: "change details unavailable"},
		syncDiffOffset: 100,
	}
	next, _ := m.syncDiffKey(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.syncDiffOffset != m.syncDiffMaxOffset() || !strings.Contains(m.View(), "change details unavailable") {
		t.Fatalf("unavailable viewer did not clamp or fail closed: offset=%d max=%d\n%s", m.syncDiffOffset, m.syncDiffMaxOffset(), m.View())
	}
}

func TestSyncDiffViewerRevealsAndDiscardsLiteralValues(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:           &cfg,
		screen:        screenSyncDiff,
		width:         100,
		height:        24,
		syncStatus:    gitops.SyncStatus{State: gitops.SyncSynced},
		syncInventory: app.SyncInventory{Available: true},
	}

	loading, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = loading.(model)
	if cmd == nil || !m.syncDiffLoading || !m.busy {
		t.Fatalf("x did not start explicit reveal: %#v", m)
	}
	revealed, _ := m.Update(syncLineDiffMsg{diff: app.SyncLineDiff{LocalEnvs: []app.LocalEnvLineDelta{{
		Project: "api", Profile: "prod", Lines: []envdiff.LineChange{
			{Kind: envdiff.LineRemoved, OldLine: 7, Text: "API_KEY=old-secret"},
			{Kind: envdiff.LineAdded, NewLine: 7, Text: "API_KEY=new-secret\x1b[31m"},
		},
	}}}})
	m = revealed.(model)
	view := m.View()
	for _, expected := range []string{"Local .env values", `-    7 │ "API_KEY=old-secret"`, `+    7 │ "API_KEY=new-secret\x1b[31m"`, "Values visible", "x hide values"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("revealed viewer missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("terminal control sequence was rendered literally:\n%s", view)
	}

	hidden, hideCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = hidden.(model)
	if hideCmd != nil || m.syncLineDiff != nil || strings.Contains(m.View(), "old-secret") {
		t.Fatalf("second x did not discard plaintext: %#v\n%s", m, m.View())
	}
	m.syncLineDiff = &app.SyncLineDiff{LocalEnvs: []app.LocalEnvLineDelta{{Project: "api"}}}
	closed, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(model).syncLineDiff != nil || closed.(model).screen != screenProjects {
		t.Fatalf("escape retained plaintext state: %#v", closed)
	}
}

func TestProfilesScreenHighlightsModifiedActiveProfile(t *testing.T) {
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{
		"api": {Path: "/tmp/api", ActiveProfile: "prod"},
	}}
	m := model{
		cfg:             &cfg,
		screen:          screenProfiles,
		width:           100,
		height:          24,
		selectedProject: "api",
		profiles:        []string{"dev", "prod"},
		statuses:        map[string]string{"api": "modified"},
		profileStatuses: map[string]map[string]string{"api": {"prod": "modified", "dev": ""}},
	}
	view := m.View()
	for _, expected := range []string{"● active", "modified", "Status"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("profiles view missing %q:\n%s", expected, view)
		}
	}
	warning := lipgloss.NewStyle().Foreground(colors.warning).Render("modified")
	if !strings.Contains(view, warning) {
		t.Fatalf("modified marker was not rendered in warning color:\n%s", view)
	}
	if strings.Contains(view, "○ matches .env") {
		t.Fatalf("inactive dev profile should not claim a disk match:\n%s", view)
	}
}

func TestProfilesScreenFlagsInactiveProfileMatchingDisk(t *testing.T) {
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{
		"api": {Path: "/tmp/api", ActiveProfile: "prod"},
	}}
	m := model{
		cfg:             &cfg,
		screen:          screenProfiles,
		width:           100,
		height:          24,
		selectedProject: "api",
		profiles:        []string{"dev", "prod"},
		statuses:        map[string]string{"api": "modified"},
		profileStatuses: map[string]map[string]string{"api": {"prod": "modified", "dev": "current"}},
	}
	view := m.View()
	if !strings.Contains(view, "○ matches .env") {
		t.Fatalf("inactive matching profile not flagged:\n%s", view)
	}
}

func TestSyncDiffViewerSelectsAndConfirmsOneEnvironmentAction(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	m := model{
		cfg:        &cfg,
		screen:     screenSyncDiff,
		width:      100,
		height:     24,
		syncStatus: gitops.SyncStatus{State: gitops.SyncSynced},
		syncInventory: app.SyncInventory{Available: true, LocalEnvs: []app.LocalEnvDelta{
			{Project: "api", Profile: "dev", Diff: envdiff.Diff{Changes: []envdiff.Change{{Key: "FIRST", Kind: envdiff.Changed}}}},
			{Project: "worker", Profile: "prod", Diff: envdiff.Diff{Changes: []envdiff.Change{{Key: "SECOND", Kind: envdiff.Changed}}}},
		}},
	}

	view := m.View()
	for _, expected := range []string{"› api / dev", "  worker / prod", "tab select env", "p publish", "d discard"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("selected viewer missing %q:\n%s", expected, view)
		}
	}
	selected, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m = selected.(model)
	if m.syncDiffSelection != 1 || !strings.Contains(m.View(), "› worker / prod") {
		t.Fatalf("tab did not select second environment: %#v\n%s", m, m.View())
	}

	publish, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = publish.(model)
	if cmd != nil || m.screen != screenConfirmDiffPublish || m.pendingDiffProject != "worker" || m.pendingDiffProfile != "prod" {
		t.Fatalf("publish did not target selected environment: %#v", m)
	}
	if !strings.Contains(m.View(), "Capture worker/prod") {
		t.Fatalf("publish confirmation omitted target:\n%s", m.View())
	}
	cancelled, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = cancelled.(model)
	if m.screen != screenSyncDiff || m.pendingDiffProject != "" {
		t.Fatalf("publish cancellation retained pending action: %#v", m)
	}

	discard, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = discard.(model)
	if cmd != nil || m.screen != screenConfirmDiffDiscard || m.pendingDiffProject != "worker" {
		t.Fatalf("discard did not target selected environment: %#v", m)
	}
	confirmed, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = confirmed.(model)
	if cmd == nil || !m.busy || m.screen != screenProjects || m.pendingDiffProject != "" {
		t.Fatalf("discard confirmation did not start selected action: %#v", m)
	}
}

func TestSyncDiffViewerBlocksPublishWhenVaultIsNotCleanAndSynced(t *testing.T) {
	m := model{
		cfg:        &vault.LocalConfig{VaultPath: "/vault"},
		screen:     screenSyncDiff,
		syncStatus: gitops.SyncStatus{State: gitops.SyncLocalAhead, Dirty: true},
		syncInventory: app.SyncInventory{LocalEnvs: []app.LocalEnvDelta{{
			Project: "api", Profile: "dev", Diff: envdiff.Diff{Changes: []envdiff.Change{{Key: "KEY", Kind: envdiff.Changed}}},
		}}},
	}
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got := next.(model)
	if cmd != nil || got.screen != screenSyncDiff || got.errText == "" {
		t.Fatalf("unsafe selected publish was not blocked: %#v", got)
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
