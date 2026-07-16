package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.screen {
	case screenOnboarding:
		return m.onboardingKey(key)
	case screenCreate, screenClone, screenAddProject, screenNewProfile,
		screenRemoteChange, screenMigrate, screenUnlockPassword,
		screenEnrollRequest, screenImportRecovery, screenRecovery:
		return m.formKey(key)
	case screenProjects:
		return m.projectsKey(key)
	case screenProfiles:
		return m.profilesKey(key)
	case screenRemote:
		return m.remoteMenuKey(key)
	case screenUnlock:
		return m.unlockMenuKey(key)
	case screenConfirmRemoveRemote:
		return m.confirmRemoveRemoteKey(key)
	case screenConfirmDisconnect:
		return m.confirmDisconnectKey(key)
	case screenConfirm:
		return m.confirmKey(key)
	case screenConfirmDelete:
		return m.confirmDeleteKey(key)
	case screenConfirmSync:
		return m.confirmSyncKey(key)
	case screenConfirmCapture:
		return m.confirmCaptureKey(key)
	case screenSyncDiff:
		return m.syncDiffKey(key)
	case screenConfirmDiffPublish:
		return m.confirmDiffActionKey(key, true)
	case screenConfirmDiffDiscard:
		return m.confirmDiffActionKey(key, false)
	case screenEditor:
		return m.editorKey(key)
	case screenConfirmEditorDiscard:
		return m.confirmEditorDiscardKey(key)
	}
	return m, nil
}

func (m model) onboardingKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(1, m.menuCursor+1)
	case "enter":
		m.openOnboardingSelection()
	}
	return m, nil
}

func (m *model) openOnboardingSelection() {
	configDir, _ := vault.ConfigDir()
	m.fieldCursor = 0
	if m.menuCursor == 0 {
		hostname, _ := os.Hostname()
		m.screen = screenCreate
		m.fields = []field{
			{"Vault directory", filepath.Join(configDir, "vault"), false},
			{"Master password", "", true},
			{"Confirm password", "", true},
			{"Device name", hostname, false},
			{"Vault sync repository (optional)", "", false},
		}
		return
	}
	m.screen = screenClone
	m.fields = []field{
		{"Vault sync repository URL", "", false},
		{"Vault directory", filepath.Join(configDir, "vault"), false},
	}
}

func (m model) formKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		return m.cancelForm()
	case "tab", "down":
		m.fieldCursor = (m.fieldCursor + 1) % len(m.fields)
	case "shift+tab", "up":
		m.fieldCursor = (m.fieldCursor + len(m.fields) - 1) % len(m.fields)
	case "enter":
		return m.submitForm()
	case "backspace":
		m.deleteLastFieldRune()
	case "ctrl+u":
		m.fields[m.fieldCursor].value = ""
	default:
		if len(key.Runes) > 0 {
			m.fields[m.fieldCursor].value += string(key.Runes)
		}
	}
	return m, nil
}

func (m model) cancelForm() (tea.Model, tea.Cmd) {
	if m.accessRequired {
		switch m.screen {
		case screenUnlockPassword, screenEnrollRequest, screenImportRecovery:
			m.screen = screenUnlock
			m.fields = nil
		case screenMigrate:
			m.errText = "migration is required before the vault can be used"
		}
		return m, nil
	}
	if m.screen == screenRemoteChange {
		m.screen = screenRemote
	} else if m.cfg.VaultPath == "" {
		m.screen = screenOnboarding
	} else if m.selectedProject != "" {
		m.screen = screenProfiles
	} else {
		m.screen = screenProjects
	}
	m.fields = nil
	return m, nil
}

func (m *model) deleteLastFieldRune() {
	value := m.fields[m.fieldCursor].value
	if value == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(value)
	m.fields[m.fieldCursor].value = value[:len(value)-size]
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	values := make([]string, len(m.fields))
	for index := range m.fields {
		values[index] = strings.TrimSpace(m.fields[index].value)
	}
	m.busy = true
	return m.submitFormValues(values)
}

func (m model) submitFormValues(values []string) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenCreate:
		return m, opCmd(func() error {
			return app.CreateProtectedVault(m.cfg, values[0], values[1], values[2], values[4], values[3], vault.StoreIdentityKeychain)
		}, "vault created")
	case screenClone:
		return m, opCmd(func() error { return app.CloneLockedVault(m.cfg, values[0], values[1]) }, "vault cloned")
	case screenAddProject:
		return m.submitProject(values[0], values[1])
	case screenNewProfile:
		m.busy = false
		return m.requestCapturePreview(m.selectedProject, values[0], captureNewProfile)
	case screenRemoteChange:
		cfg := *m.cfg
		return m, opCmd(func() error { return app.ConfigureVaultRemote(cfg, values[0]) }, "vault sync repository configured")
	case screenMigrate:
		cfg := *m.cfg
		return m, opCmd(func() error {
			return app.MigrateVaultAccess(cfg, values[0], values[1], values[2], vault.StoreIdentityKeychain)
		}, "vault access migrated")
	case screenUnlockPassword:
		vaultPath := m.cfg.VaultPath
		return m, opCmd(func() error { return vault.UnlockVault(vaultPath, values[0], vault.StoreIdentityKeychain) }, "vault unlocked")
	case screenEnrollRequest:
		return m.submitEnrollment(values[0])
	case screenImportRecovery:
		return m, opCmd(func() error { return app.ImportIdentityValue(values[0]) }, "recovery identity imported")
	case screenRecovery:
		return m, opCmd(func() error { return app.ExportIdentity(values[0]) }, "recovery identity exported")
	}
	m.busy = false
	return m, nil
}

func (m model) submitProject(projectName, profileName string) (tea.Model, tea.Cmd) {
	if _, exists := m.manifest.Projects[projectName]; exists {
		return m, opCmd(func() error {
			return app.LinkExistingProject(m.cfg, m.current, projectName, profileName)
		}, "existing project linked and profile applied")
	}
	m.busy = false
	return m.requestCapturePreview(projectName, profileName, captureNewProject)
}

func (m model) submitEnrollment(deviceName string) (tea.Model, tea.Cmd) {
	cfg := m.cfg
	return m, func() tea.Msg {
		request, err := app.RequestDeviceEnrollment(cfg, deviceName)
		if err != nil {
			return operationMsg{err: err}
		}
		return operationMsg{info: "enrollment requested — ID: " + request.ID}
	}
}

func (m model) projectsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.projectCursor = max(0, m.projectCursor-1)
	case "down", "j":
		m.projectCursor = min(max(0, len(m.projects)-1), m.projectCursor+1)
	case "enter":
		m.openSelectedProject()
	case "a":
		m.openAddProject()
	case "c":
		return m.captureSelectedProject()
	case "p":
		return m.syncVault(false)
	case "u":
		return m.syncVault(true)
	case "s":
		return m.requestContextualSync()
	case "v":
		m.screen = screenSyncDiff
		m.syncDiffOffset = 0
		m.syncDiffReturn = screenProjects
	case "g":
		m.screen, m.menuCursor = screenRemote, 0
	case "b":
		home, _ := os.UserHomeDir()
		m.screen = screenRecovery
		m.fields = []field{{"Recovery backup path", filepath.Join(home, "gitenv-recovery.txt"), false}}
		m.fieldCursor = 0
	case "r":
		m.syncStatus.State = gitops.SyncChecking
		return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
	}
	return m, nil
}

func (m model) syncDiffKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	pageSize := m.syncDiffPageSize()
	m.syncDiffOffset = clampSyncDiffOffset(m.syncDiffOffset, len(m.syncDiffLines()), pageSize)
	switch key.String() {
	case "esc", "q":
		m.screen = m.syncDiffReturnScreen()
		m.syncDiffOffset = 0
		m.syncLineDiff = nil
		m.syncDiffLoading = false
	case "x":
		if m.syncDiffLoading {
			return m, nil
		}
		if m.syncLineDiff != nil {
			m.syncLineDiff = nil
			m.syncDiffOffset = 0
			return m, nil
		}
		m.busy = true
		m.syncDiffLoading = true
		m.errText = ""
		return m, revealSyncDiffCmd(m.cfg, m.syncStatus, m.syncDiffScope())
	case "tab":
		m = m.selectNextSyncDiff(1)
	case "shift+tab":
		m = m.selectNextSyncDiff(-1)
	case "p":
		return m.requestDiffPublish()
	case "d":
		return m.requestDiffDiscard()
	case "e":
		if target, ok := m.selectedSyncDiffTarget(); ok {
			return m.openEditor(target.project, screenSyncDiff)
		}
	case "up", "k":
		m.syncDiffOffset = max(0, m.syncDiffOffset-1)
	case "down", "j":
		m.syncDiffOffset = min(m.syncDiffMaxOffset(), m.syncDiffOffset+1)
	case "pgup", "ctrl+b":
		m.syncDiffOffset = max(0, m.syncDiffOffset-pageSize)
	case "pgdown", "ctrl+f", " ":
		m.syncDiffOffset = min(m.syncDiffMaxOffset(), m.syncDiffOffset+pageSize)
	case "home", "g":
		m.syncDiffOffset = 0
	case "end", "G":
		m.syncDiffOffset = m.syncDiffMaxOffset()
	}
	return m, nil
}

func (m *model) openSelectedProject() {
	if len(m.projects) == 0 {
		return
	}
	m.selectedProject = m.projects[m.projectCursor]
	m.profiles = sortedKeys(m.manifest.Projects[m.selectedProject].Profiles)
	m.profileCursor = 0
	m.screen = screenProfiles
}

func (m *model) openAddProject() {
	if !m.current.HasEnv {
		m.errText = "current directory has no .env file"
		return
	}
	if m.current.LinkedName != "" {
		m.errText = "current directory is already linked as " + m.current.LinkedName
		return
	}
	projectName, profileName := m.current.Name, "dev"
	if candidate, exists := m.manifest.Projects[m.current.Name]; exists {
		if _, linked := m.cfg.Projects[m.current.Name]; !linked {
			profiles := sortedKeys(candidate.Profiles)
			if len(profiles) > 0 {
				profileName = profiles[0]
			}
		}
	}
	m.screen = screenAddProject
	m.fields = []field{{"Project name", projectName, false}, {"Initial profile", profileName, false}}
	m.fieldCursor = 0
}

func (m model) captureSelectedProject() (tea.Model, tea.Cmd) {
	if len(m.projects) == 0 {
		return m, nil
	}
	project := m.projects[m.projectCursor]
	active := m.cfg.Projects[project].ActiveProfile
	if active == "" {
		m.errText = "project has no active profile"
		return m, nil
	}
	return m.requestCapturePreview(project, active, captureExistingProfile)
}

func (m model) syncVault(push bool) (tea.Model, tea.Cmd) {
	if !app.HasRemote(*m.cfg) {
		m.errText = "configure a vault sync repository first with g"
		return m, nil
	}
	m.busy = true
	if push {
		return m, opCmd(func() error { return app.Push(*m.cfg) }, "vault pushed")
	}
	return m, opCmd(func() error { return app.Pull(*m.cfg) }, "vault pulled; local .env files unchanged")
}

func (m model) remoteMenuKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(3, m.menuCursor+1)
	case "enter":
		return m.selectRemoteMenuItem()
	}
	return m, nil
}

func (m model) selectRemoteMenuItem() (tea.Model, tea.Cmd) {
	switch m.menuCursor {
	case 0:
		m.screen = screenRemoteChange
		m.fields = []field{{"Vault sync repository URL", m.remoteURL, false}}
		m.fieldCursor = 0
	case 1:
		cfg := *m.cfg
		m.busy = true
		return m, opCmd(func() error { return app.TestVaultRemote(cfg) }, "vault sync repository is reachable")
	case 2:
		m.screen = screenConfirmRemoveRemote
	case 3:
		m.screen = screenProjects
	}
	return m, nil
}

func (m model) unlockMenuKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.errText = "unlock or disconnect the vault before continuing"
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(3, m.menuCursor+1)
	case "enter":
		return m.selectUnlockMenuItem()
	}
	return m, nil
}

func (m model) selectUnlockMenuItem() (tea.Model, tea.Cmd) {
	switch m.menuCursor {
	case 0:
		if m.migrationRecoveryRequired {
			m.errText = "this legacy vault has no master password; use recovery or device approval"
			return m, nil
		}
		m.screen = screenUnlockPassword
		m.fields = []field{{"Master password", "", true}}
		m.fieldCursor = 0
	case 1:
		if m.cfg.PendingEnrollmentID != "" {
			cfg, requestID := m.cfg, m.cfg.PendingEnrollmentID
			m.busy = true
			return m, opCmd(func() error {
				return app.ActivateDeviceEnrollment(cfg, requestID, vault.StoreIdentityKeychain)
			}, "device enrollment activated; vault is now accessible")
		}
		hostname, _ := os.Hostname()
		m.screen = screenEnrollRequest
		m.fields = []field{{"Device name", hostname, false}}
		m.fieldCursor = 0
	case 2:
		m.screen = screenImportRecovery
		m.fields = []field{{"Recovery key", "", true}}
		m.fieldCursor = 0
	case 3:
		m.screen = screenConfirmDisconnect
	}
	return m, nil
}

// isFocusedProject reports whether the TUI launched inside a linked project and
// the user has not unlocked browsing across all projects yet.
func (m model) isFocusedProject() bool {
	return m.current.LinkedName != "" && !m.browseProjects
}

func (m model) profilesKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc":
		if m.isFocusedProject() {
			return m, tea.Quit
		}
		m.screen, m.selectedProject = screenProjects, ""
	case "p":
		m.browseProjects = true
		m.screen, m.selectedProject = screenProjects, ""
	case "up", "k":
		m.profileCursor = max(0, m.profileCursor-1)
	case "down", "j":
		m.profileCursor = min(max(0, len(m.profiles)-1), m.profileCursor+1)
	case "enter":
		return m.applySelectedProfile()
	case "c":
		return m.captureActiveProfile()
	case "n":
		m.screen = screenNewProfile
		m.fields = []field{{"New profile name", "", false}}
		m.fieldCursor = 0
	case "e":
		return m.openEditor(m.selectedProject, screenProfiles)
	case "d":
		m.requestProfileRemoval()
	case "s":
		return m.requestContextualSync()
	case "v":
		m.screen = screenSyncDiff
		m.syncDiffOffset = 0
		m.syncDiffReturn = screenProfiles
	case "r":
		m.syncStatus.State = gitops.SyncChecking
		return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
	}
	return m, nil
}

func (m model) applySelectedProfile() (tea.Model, tea.Cmd) {
	if len(m.profiles) == 0 {
		return m, nil
	}
	profile := m.profiles[m.profileCursor]
	status := m.statuses[m.selectedProject]
	if status == "modified" || status == "unmanaged" {
		m.pendingProfile, m.screen = profile, screenConfirm
		return m, nil
	}
	project := m.selectedProject
	m.busy = true
	return m, opCmd(func() error { return vault.Apply(m.cfg, project, profile, false) }, "profile applied")
}

func (m model) captureActiveProfile() (tea.Model, tea.Cmd) {
	active := m.cfg.Projects[m.selectedProject].ActiveProfile
	if active == "" {
		m.errText = "project has no active profile"
		return m, nil
	}
	return m.requestCapturePreview(m.selectedProject, active, captureExistingProfile)
}

func (m *model) requestProfileRemoval() {
	if len(m.profiles) == 0 {
		return
	}
	profile := m.profiles[m.profileCursor]
	if m.cfg.Projects[m.selectedProject].ActiveProfile == profile {
		m.errText = "active profile cannot be removed; apply another profile first"
		return
	}
	m.pendingProfile, m.screen = profile, screenConfirmDelete
}

func (m model) confirmKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.screen, m.busy = screenProfiles, true
		return m, opCmd(func() error { return vault.Apply(m.cfg, project, profile, true) }, "profile applied; local changes discarded")
	}
	m.screen, m.pendingProfile, m.info = screenProfiles, "", "cancelled"
	return m, nil
}

func (m model) confirmDeleteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.pendingProfile, m.screen, m.busy = "", screenProfiles, true
		return m, opCmd(func() error { return vault.RemoveProfile(m.cfg, project, profile) }, "profile removed")
	}
	m.screen, m.pendingProfile, m.info = screenProfiles, "", "cancelled"
	return m, nil
}

func (m model) confirmRemoveRemoteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		cfg := *m.cfg
		m.busy = true
		return m, opCmd(func() error { return app.RemoveVaultRemote(cfg) }, "vault sync repository removed")
	}
	m.screen, m.info = screenRemote, "cancelled"
	return m, nil
}

func (m model) confirmDisconnectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		m.screen, m.busy = screenOnboarding, true
		return m, opCmd(func() error { return app.DisconnectVault(m.cfg) }, "vault disconnected from this computer")
	}
	m.screen, m.info = screenUnlock, "cancelled"
	return m, nil
}
