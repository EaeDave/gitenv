package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func (m model) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.screen == screenProjects && m.projectList != nil && m.projectList.SettingFilter() {
		return m.projectsKey(key)
	}
	// `U` stays the self-update key. It is safe to keep next to nothing now:
	// the old lowercase `u` (publish vault) has been removed, so a slipped
	// shift can no longer turn "publish my vault" into "replace the binary and
	// relaunch". Explicit pull/push live in the sync actions menu instead.
	if key.String() == "U" && m.updateAvailable && !m.updating {
		switch m.screen {
		case screenOnboarding, screenProjects, screenProfiles:
			return m.beginSelfUpdate()
		}
	}
	// `?` opens the full keymap and glossary for the screen it was pressed on.
	// Forms are excluded because there `?` is literal text.
	if key.String() == "?" {
		switch m.screen {
		case screenProjects, screenProfiles, screenSyncDiff, screenDevices, screenDiverged:
			m.helpReturn = m.screen
			m.screen = screenHelp
			return m, nil
		}
	}
	switch m.screen {
	case screenOnboarding:
		return m.onboardingKey(key)
	case screenCreate, screenClone, screenAddProject, screenNewProfile,
		screenRemoteChange, screenMigrate, screenUnlockPassword,
		screenEnrollRequest, screenImportRecovery, screenRecovery,
		screenRecoveryPrompt,
		screenAdoptClone, screenAdoptLink, screenProjectOptions:
		return m.formKey(key)
	case screenProjects:
		return m.projectsKey(key)
	case screenAdoptCandidates:
		return m.adoptCandidatesKey(key)
	case screenAdoptProfile:
		return m.adoptProfileKey(key)
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
	case screenDevices:
		return m.devicesKey(key)
	case screenConfirmApprove:
		return m.confirmApproveKey(key)
	case screenConfirmReject:
		return m.confirmRejectKey(key)
	case screenDiverged:
		return m.divergedKey(key)
	case screenDivergedProfiles:
		return m.divergedProfilesKey(key)
	case screenConfirmDiverged:
		return m.confirmDivergedKey(key)
	case screenSyncActions:
		return m.syncActionsKey(key)
	case screenHelp:
		return m.helpKey(key)
	}
	return m, nil
}

func (m model) onboardingKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) formKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		if text := sanitizeInput(key.Text); text != "" {
			m.fields[m.fieldCursor].value += text
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
			// Migration cannot be skipped, so cancelling returns to the unlock
			// menu, where "Disconnect this vault" is a real way out. Previously
			// this printed an error and left the user on a screen whose own help
			// line promised esc would cancel.
			m.screen = screenUnlock
			m.menuCursor = 0
			m.fields = nil
			m.info = "migration postponed — the vault stays locked until it is migrated"
		}
		return m, nil
	}
	switch m.screen {
	case screenAdoptClone, screenAdoptLink:
		m.screen = screenProjects
		m.fields = nil
		return m, nil
	case screenProjectOptions:
		m.screen = m.optionsReturnScreen()
		m.fields = nil
		return m, nil
	case screenRecoveryPrompt:
		// Skipping the recovery backup is allowed, but the header keeps warning
		// until a key has actually been exported.
		m.screen = screenProjects
		m.fields = nil
		m.errText = "no recovery key saved — press b to save one; without it a forgotten password is unrecoverable"
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

// sanitizeInput drops control and non-printable runes (including the NUL bytes
// the Windows console can inject on some keystrokes/pastes) so they never reach
// a git argument, where they would make the OS reject the process with
// "invalid argument".
func sanitizeInput(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	values := make([]string, len(m.fields))
	for index := range m.fields {
		values[index] = strings.TrimSpace(sanitizeInput(m.fields[index].value))
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
		cfg := m.cfg
		return m, opCmd(func() error { return app.ImportRecoveryKey(cfg, values[0]) }, "recovery key accepted")
	case screenRecovery, screenRecoveryPrompt:
		cfg := m.cfg
		return m, opCmd(func() error {
			return app.ExportRecoveryKey(cfg, values[0])
		}, "recovery key saved — keep it somewhere other than this computer")
	case screenAdoptClone, screenAdoptLink:
		if values[0] == "" {
			m.busy = false
			if m.screen == screenAdoptClone {
				m.errText = "destination directory is required"
			} else {
				m.errText = "project directory is required"
			}
			return m, nil
		}
		m.busy = false
		return m.beginAdopt(values[0])
	case screenProjectOptions:
		name, envFile, endings := m.adoptName, values[0], values[1]
		cfg := *m.cfg
		return m, opCmd(func() error {
			policy, err := vault.ParseLineEndingPolicy(endings)
			if err != nil {
				return err
			}
			if err := app.SetProjectEnvFile(cfg, name, envFile); err != nil {
				return err
			}
			return app.SetProjectLineEndings(cfg, name, policy)
		}, "project options updated")
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
		if _, err := app.RequestDeviceEnrollment(cfg, deviceName); err != nil {
			return operationMsg{err: err}
		}
		// The request id is deliberately not shown. It is stored locally and
		// the approving computer now finds the request by itself, so making the
		// user copy a 32-character hex string between machines is pure friction.
		return operationMsg{info: "approval requested — open gitenv on an enrolled computer and approve this device"}
	}
}

func (m model) projectsKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if updated, cmd, handled := m.updateProjectList(key); handled {
		return updated, cmd
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "tab":
		m.cycleProjectFilter(1)
	case "shift+tab":
		m.cycleProjectFilter(-1)
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
	case "f":
		// Find local clones. This was `d` for one release; `d` means "remove"
		// on the profiles screen and "discard" in the diff viewer, so reusing it
		// for a scan made one letter mean three things, two of them destructive.
		m.busy = true
		return m, discoverCmd(m.cfg)
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
		m.fields = []field{{"Recovery key file", filepath.Join(home, "gitenv-recovery.txt"), false}}
		m.fieldCursor = 0
	case "d":
		m.screen, m.menuCursor, m.approvalCursor = screenDevices, 0, 0
	case "o":
		m.openProjectOptions()
	case "r":
		m.syncStatus.State = gitops.SyncChecking
		return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
	}
	return m, nil
}

func (m model) syncDiffKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "pgdown", "ctrl+f", "space":
		m.syncDiffOffset = min(m.syncDiffMaxOffset(), m.syncDiffOffset+pageSize)
	case "home", "g":
		m.syncDiffOffset = 0
	case "end", "G":
		m.syncDiffOffset = m.syncDiffMaxOffset()
	}
	return m, nil
}

func (m *model) openSelectedProject() {
	if item, ok := m.selectedProjectListItem(); ok && item.current {
		m.openAddProject()
		return
	}
	state, ok := m.selectedProjectState()
	if !ok {
		return
	}
	switch state.Kind {
	case app.ProjectLinked:
		m.selectedProject = state.Name
		m.profiles = sortedKeys(m.manifest.Projects[state.Name].Profiles)
		m.profileCursor = 0
		m.screen = screenProfiles
	case app.ProjectMissing:
		m.openAdoptClone(state)
	case app.ProjectFound:
		if len(state.Candidates) > 1 {
			m.openAdoptCandidates(state)
		} else {
			path := ""
			if len(state.Candidates) == 1 {
				path = state.Candidates[0]
			}
			m.openAdoptLink(state, path)
		}
	case app.ProjectNoRepo:
		m.openAdoptLink(state, "")
	}
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
	if item, ok := m.selectedProjectListItem(); ok && item.current {
		m.openAddProject()
		return m, nil
	}
	state, ok := m.selectedProjectState()
	if !ok {
		return m, nil
	}
	if state.Kind != app.ProjectLinked {
		m.errText = "press enter to adopt this project before capturing"
		return m, nil
	}
	active := m.cfg.Projects[state.Name].ActiveProfile
	if active == "" {
		m.errText = "project has no active profile"
		return m, nil
	}
	return m.requestCapturePreview(state.Name, active, captureExistingProfile)
}

// openProjectOptions opens the env-file / line-endings form for the selected
// vault project. It works whether or not the project is linked here, because
// both settings live in the encrypted project metadata, not the local link.
func (m *model) openProjectOptions() {
	if item, ok := m.selectedProjectListItem(); ok && item.current {
		m.errText = "add the current project before changing its options"
		return
	}
	state, ok := m.selectedProjectState()
	if !ok {
		return
	}
	m.openProjectOptionsState(state)
}

// openProjectOptionsFor opens the options form for a project addressed by name,
// so the profiles screen can reach it without the project list being visible.

func (m *model) openProjectOptionsFor(name string) {
	state, ok := m.projectStateByName(name)
	if !ok {
		m.errText = "project options are unavailable until the vault finishes loading"
		return
	}
	m.openProjectOptionsState(state)
}

func (m *model) openProjectOptionsState(state app.ProjectState) {
	envFile := state.EnvFile
	if envFile == "" {
		envFile = vault.DefaultEnvFile
	}
	m.adoptName = state.Name
	m.optionsReturn = m.screen
	m.screen = screenProjectOptions
	m.fields = []field{
		{"Env file", envFile, false},
		{"Line endings", state.LineEndings.String(), false},
	}
	m.fieldCursor = 0
}

// optionsReturnScreen is the screen the project options form returns to. It is
// the profiles screen when options were opened from inside a project, so saving
// or cancelling does not eject the user out to the project list.
func (m model) optionsReturnScreen() screen {
	if m.optionsReturn == screenProfiles && m.selectedProject != "" {
		return screenProfiles
	}
	return screenProjects
}

// openAdoptClone opens the clone-and-adopt form for a project with a recorded
// repository but no local clone, prefilling the workspace destination.
func (m *model) openAdoptClone(state app.ProjectState) {
	m.adoptName = state.Name
	m.screen = screenAdoptClone
	m.fields = []field{{"Destination directory", app.SuggestCloneDest(*m.cfg, state.Name), false}}
	m.fieldCursor = 0
}

// openAdoptLink opens the link form for a project whose clone is already on
// disk, prefilling the discovered path when there is exactly one candidate.
func (m *model) openAdoptLink(state app.ProjectState, path string) {
	m.adoptName = state.Name
	m.screen = screenAdoptLink
	m.fields = []field{{"Project directory", path, false}}
	m.fieldCursor = 0
}

// beginAdopt opens a closed-set profile picker when a project has several
// profiles. A single profile bypasses the extra screen without weakening the
// invariant: only names loaded from the manifest reach the adoption command.
func (m model) beginAdopt(path string) (tea.Model, tea.Cmd) {
	m.adoptPath = path
	m.adoptProfiles = sortedKeys(m.manifest.Projects[m.adoptName].Profiles)
	if len(m.adoptProfiles) > 1 {
		m.adoptReturn = m.screen
		m.menuCursor = 0
		m.screen = screenAdoptProfile
		return m, nil
	}
	profile := ""
	if len(m.adoptProfiles) == 1 {
		profile = m.adoptProfiles[0]
	}
	return m.startAdopt(profile)
}

// startAdopt dispatches the operation remembered by adoptReturn/current screen.
func (m model) startAdopt(profile string) (tea.Model, tea.Cmd) {
	origin := m.screen
	if origin == screenAdoptProfile {
		origin = m.adoptReturn
	}
	m.busy = true
	m.screen = screenProjects
	if origin == screenAdoptClone {
		return m, cloneAdoptCmd(m.cfg, m.adoptName, m.adoptPath, profile)
	}
	return m, linkAdoptCmd(m.cfg, m.adoptName, m.adoptPath, profile)
}

// adoptProfileKey selects from manifest-backed profile names; free-form input
// is intentionally impossible because a typo would only fail after clone work.
func (m model) adoptProfileKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.screen = m.adoptReturn
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(max(0, len(m.adoptProfiles)-1), m.menuCursor+1)
	case "enter":
		if len(m.adoptProfiles) == 0 {
			return m, nil
		}
		return m.startAdopt(m.adoptProfiles[m.menuCursor])
	}
	return m, nil
}

// openAdoptCandidates opens the picker for a project with several local clones.
func (m *model) openAdoptCandidates(state app.ProjectState) {
	m.adoptName = state.Name
	m.adoptCandidates = state.Candidates
	m.menuCursor = 0
	m.screen = screenAdoptCandidates
}

// adoptCandidatesKey drives the discovered-clone picker: pick one path to link.
func (m model) adoptCandidatesKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(max(0, len(m.adoptCandidates)-1), m.menuCursor+1)
	case "enter":
		if len(m.adoptCandidates) == 0 {
			return m, nil
		}
		path := m.adoptCandidates[m.menuCursor]
		state, ok := m.projectStateByName(m.adoptName)
		if !ok {
			m.errText = "project is unavailable until the vault finishes loading"
			return m, nil
		}
		m.openAdoptLink(state, path)
		return m, nil
	}
	return m, nil
}

func (m model) remoteMenuKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		m.fields = []field{{label: "Vault sync repository URL", value: m.remoteURL}}
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

func (m model) unlockMenuKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		// A locked vault has nothing to go back to, so the honest exit is to
		// leave. The help line says "quit" to match, instead of promising a
		// "back" that only produced an error.
		return m, tea.Quit
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
		// Not masked: this is a long key pasted from a password manager, and
		// asterisks only stop the user from noticing a truncated or mangled
		// paste. Anyone who can read the screen now could read it there too.
		m.fields = []field{{"Recovery key", "", false}}
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

func (m model) profilesKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "o":
		// Reachable from here too: in focus mode the project list is hidden, so
		// otherwise changing this project's env file meant leaving focus, finding
		// the project in the list, and pressing o there.
		m.openProjectOptionsFor(m.selectedProject)
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

func (m model) confirmKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.screen, m.busy = screenProfiles, true
		return m, opCmd(func() error { return vault.Apply(m.cfg, project, profile, true) }, "profile applied; local changes discarded")
	}
	m.screen, m.pendingProfile, m.info = screenProfiles, "", "cancelled"
	return m, nil
}

func (m model) confirmDeleteKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.pendingProfile, m.screen, m.busy = "", screenProfiles, true
		return m, opCmd(func() error { return vault.RemoveProfile(m.cfg, project, profile) }, "profile removed")
	}
	m.screen, m.pendingProfile, m.info = screenProfiles, "", "cancelled"
	return m, nil
}

func (m model) confirmRemoveRemoteKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		cfg := *m.cfg
		m.busy = true
		return m, opCmd(func() error { return app.RemoveVaultRemote(cfg) }, "vault sync repository removed")
	}
	m.screen, m.info = screenRemote, "cancelled"
	return m, nil
}

func (m model) confirmDisconnectKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		m.screen, m.busy = screenOnboarding, true
		return m, opCmd(func() error { return app.DisconnectVault(m.cfg) }, "vault disconnected from this computer")
	}
	m.screen, m.info = screenUnlock, "cancelled"
	return m, nil
}
