// Package tui provides gitenv's primary terminal interface.
package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

type screen int

const (
	screenOnboarding screen = iota
	screenCreate
	screenClone
	screenProjects
	screenAddProject
	screenProfiles
	screenNewProfile
	screenRemote              // cursor menu: Change / Test / Remove / Back
	screenRemoteChange        // form: vault sync repository URL
	screenConfirmRemoveRemote // y/N: remove vault sync repository
	screenMigrate             // form: password / confirm / device — protect unprotected vault
	screenUnlock              // cursor menu: password / enrollment / import-recovery
	screenUnlockPassword      // form: master password (masked)
	screenEnrollRequest       // form: device name → RequestDeviceEnrollment
	screenImportRecovery      // form: pasted recovery identity → ImportIdentityValue
	screenRecovery            // form: export recovery identity (b key)
	screenConfirmDisconnect
	screenConfirm
	screenConfirmDelete
)

type field struct {
	label  string
	value  string
	masked bool
}

type operationMsg struct {
	info string
	err  error
}

type reloadMsg struct {
	manifest                 vault.Manifest
	statuses                 map[string]string
	current                  app.CurrentProject
	remoteURL                string // raw URL, only when safe to prefill (no credentials)
	remoteDisplayURL         string // redacted URL for display
	needsMigration           bool
	migrationIdentityMissing bool
	needsUnlock              bool
}

type model struct {
	cfg                                                   *vault.LocalConfig
	cwd                                                   string
	current                                               app.CurrentProject
	manifest                                              vault.Manifest
	statuses                                              map[string]string
	projects, profiles                                    []string
	projectCursor, profileCursor, menuCursor, fieldCursor int
	selectedProject, pendingProfile                       string
	screen                                                screen
	fields                                                []field
	info, errText                                         string
	busy                                                  bool
	remoteURL                                             string // safe prefill URL (no embedded credentials)
	remoteDisplayURL                                      string // redacted display URL
	accessRequired                                        bool
	migrationRecoveryRequired                             bool
}

func newModel(cfg *vault.LocalConfig, cwd string) model {
	m := model{cfg: cfg, cwd: cwd, statuses: map[string]string{}}
	current, err := app.DetectCurrent(*cfg, cwd)
	if err == nil {
		m.current = current
	}
	if cfg.VaultPath == "" {
		m.screen = screenOnboarding
	} else {
		m.screen = screenProjects
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.cfg.VaultPath == "" {
		return nil
	}
	return loadCmd(m.cfg, m.cwd)
}

func loadCmd(cfg *vault.LocalConfig, cwd string) tea.Cmd {
	return func() tea.Msg {
		manifest, err := vault.LoadManifest(cfg.VaultPath)
		if err != nil {
			return operationMsg{err: err}
		}
		current, err := app.DetectCurrent(*cfg, cwd)
		if err != nil {
			return operationMsg{err: err}
		}
		identity, identityErr := vault.LoadIdentityForManifest(manifest)
		identityAllowed := identityErr == nil && identity != nil
		if manifest.WrappedIdentity == nil {
			if !identityAllowed {
				return reloadMsg{manifest: manifest, current: current, migrationIdentityMissing: true}
			}
			return reloadMsg{manifest: manifest, current: current, needsMigration: true}
		}
		if !identityAllowed {
			return reloadMsg{manifest: manifest, current: current, needsUnlock: true}
		}
		if current.LinkedName == "" {
			if match := app.MatchVaultProject(manifest, current); match != "" {
				if err := vault.Link(cfg, match, current.Path); err != nil {
					return operationMsg{err: err}
				}
				local := cfg.Projects[match]
				local.RepositoryIdentity = current.RepositoryIdentity
				cfg.Projects[match] = local
				if err := vault.SaveLocal(*cfg); err != nil {
					return operationMsg{err: err}
				}
				current.LinkedName = match
			}
		}
		statuses := make(map[string]string, len(cfg.Projects))
		for name := range cfg.Projects {
			status, statusErr := vault.Status(*cfg, name)
			if statusErr != nil {
				statuses[name] = "error"
			} else {
				statuses[name] = status
			}
		}
		rawURL, rawErr := app.VaultRemoteURL(*cfg)
		displayURL := app.VaultRemoteDisplayURL(*cfg)
		prefillURL := ""
		if rawErr == nil && rawURL == displayURL {
			prefillURL = rawURL
		}
		return reloadMsg{
			manifest:         manifest,
			statuses:         statuses,
			current:          current,
			remoteURL:        prefillURL,
			remoteDisplayURL: displayURL,
		}
	}
}

func opCmd(fn func() error, info string) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return operationMsg{err: err}
		}
		return operationMsg{info: info}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case reloadMsg:
		m.busy = false
		m.manifest = msg.manifest
		m.statuses = msg.statuses
		m.projects = sortedKeys(m.cfg.Projects)
		m.current = msg.current
		m.remoteURL = msg.remoteURL
		m.remoteDisplayURL = msg.remoteDisplayURL
		if m.selectedProject != "" {
			m.profiles = sortedKeys(m.manifest.Projects[m.selectedProject].Profiles)
			if m.profileCursor >= len(m.profiles) {
				m.profileCursor = max(0, len(m.profiles)-1)
			}
		}
		m.accessRequired = msg.migrationIdentityMissing || msg.needsMigration || msg.needsUnlock
		m.migrationRecoveryRequired = msg.migrationIdentityMissing
		if msg.migrationIdentityMissing {
			m.screen = screenUnlock
			m.menuCursor = 0
			m.fields = nil
			m.errText = "vault recovery identity is required before migration"
			return m, nil
		}
		if msg.needsMigration {
			hostname, _ := os.Hostname()
			m.screen = screenMigrate
			m.fields = []field{
				{"Master password", "", true},
				{"Confirm password", "", true},
				{"Device name", hostname, false},
			}
			m.fieldCursor = 0
			return m, nil
		}
		if msg.needsUnlock {
			m.screen = screenUnlock
			m.menuCursor = 0
			m.fields = nil
			return m, nil
		}
		m.accessRequired = false
		m.migrationRecoveryRequired = false
		return m, nil

	case operationMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = safeError(msg.err)
			return m, nil
		}
		m.info = msg.info
		if m.cfg.VaultPath != "" {
			switch m.screen {
			case screenCreate, screenClone,
				screenAddProject, screenNewProfile,
				screenRemoteChange, screenConfirmRemoveRemote,
				screenMigrate, screenUnlock, screenUnlockPassword,
				screenImportRecovery, screenRecovery:
				m.screen = screenProjects
			case screenEnrollRequest:
				m.screen = screenUnlock
			}
			return m, loadCmd(m.cfg, m.cwd)
		}

	case tea.KeyMsg:
		if m.busy {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		m.info = ""
		m.errText = ""
		return m.handleKey(msg)
	}
	return m, nil
}

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
	}
	return m, nil
}

func (m model) onboardingKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := 2
	switch key.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < items-1 {
			m.menuCursor++
		}
	case "enter":
		configDir, _ := vault.ConfigDir()
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
		} else {
			m.screen = screenClone
			m.fields = []field{
				{"Vault sync repository URL", "", false},
				{"Vault directory", filepath.Join(configDir, "vault"), false},
			}
		}
		m.fieldCursor = 0
	}
	return m, nil
}

func (m model) formKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		if m.accessRequired {
			switch m.screen {
			case screenUnlockPassword, screenEnrollRequest:
				m.screen = screenUnlock
				m.fields = nil
			case screenImportRecovery:
				m.screen = screenUnlock
				m.fields = nil
			case screenMigrate:
				m.errText = "migration is required before the vault can be used"
			}
			return m, nil
		}
		switch m.screen {
		case screenRemoteChange:
			m.screen = screenRemote
		default:
			if m.cfg.VaultPath == "" {
				m.screen = screenOnboarding
			} else if m.selectedProject != "" {
				m.screen = screenProfiles
			} else {
				m.screen = screenProjects
			}
		}
		m.fields = nil
		return m, nil
	case "tab", "down":
		m.fieldCursor = (m.fieldCursor + 1) % len(m.fields)
	case "shift+tab", "up":
		m.fieldCursor = (m.fieldCursor + len(m.fields) - 1) % len(m.fields)
	case "enter":
		return m.submitForm()
	case "backspace":
		if value := m.fields[m.fieldCursor].value; value != "" {
			_, size := utf8.DecodeLastRuneInString(value)
			m.fields[m.fieldCursor].value = value[:len(value)-size]
		}
	case "ctrl+u":
		m.fields[m.fieldCursor].value = ""
	default:
		if len(key.Runes) > 0 {
			m.fields[m.fieldCursor].value += string(key.Runes)
		}
	}
	return m, nil
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	values := make([]string, len(m.fields))
	for i := range m.fields {
		values[i] = strings.TrimSpace(m.fields[i].value)
	}
	m.busy = true
	switch m.screen {
	case screenCreate:
		// [0]=dir [1]=password [2]=confirm [3]=device [4]=remote
		return m, opCmd(func() error {
			return app.CreateProtectedVault(m.cfg, values[0], values[1], values[2], values[4], values[3], vault.StoreIdentityKeychain)
		}, "vault created")
	case screenClone:
		// [0]=url [1]=dir
		return m, opCmd(func() error {
			return app.CloneLockedVault(m.cfg, values[0], values[1])
		}, "vault cloned")
	case screenAddProject:
		if _, exists := m.manifest.Projects[values[0]]; exists {
			return m, opCmd(func() error {
				return app.LinkExistingProject(m.cfg, m.current, values[0], values[1])
			}, "existing project linked and profile applied")
		}
		return m, opCmd(func() error {
			return app.AddCurrentProject(m.cfg, m.current, values[0], values[1])
		}, "new project added and .env captured")
	case screenNewProfile:
		project := m.selectedProject
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, values[0]) }, "new profile captured")
	case screenRemoteChange:
		cfg := *m.cfg
		return m, opCmd(func() error {
			return app.ConfigureVaultRemote(cfg, values[0])
		}, "vault sync repository configured")
	case screenMigrate:
		// [0]=password [1]=confirm [2]=device
		cfg := *m.cfg
		return m, opCmd(func() error {
			return app.MigrateVaultAccess(cfg, values[0], values[1], values[2], vault.StoreIdentityKeychain)
		}, "vault access migrated")
	case screenUnlockPassword:
		vaultPath := m.cfg.VaultPath
		return m, opCmd(func() error {
			return vault.UnlockVault(vaultPath, values[0], vault.StoreIdentityKeychain)
		}, "vault unlocked")
	case screenEnrollRequest:
		cfg := m.cfg
		name := values[0]
		return m, func() tea.Msg {
			req, err := app.RequestDeviceEnrollment(cfg, name)
			if err != nil {
				return operationMsg{err: err}
			}
			return operationMsg{info: "enrollment requested — ID: " + req.ID}
		}
	case screenImportRecovery:
		return m, opCmd(func() error { return app.ImportIdentityValue(values[0]) }, "recovery identity imported")
	case screenRecovery:
		return m, opCmd(func() error { return app.ExportIdentity(values[0]) }, "recovery identity exported")
	}
	m.busy = false
	return m, nil
}

func (m model) projectsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case "down", "j":
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
		}
	case "enter":
		if len(m.projects) > 0 {
			m.selectedProject = m.projects[m.projectCursor]
			m.profiles = sortedKeys(m.manifest.Projects[m.selectedProject].Profiles)
			m.profileCursor = 0
			m.screen = screenProfiles
		}
	case "a":
		if !m.current.HasEnv {
			m.errText = "current directory has no .env file"
			break
		}
		if m.current.LinkedName != "" {
			m.errText = "current directory is already linked as " + m.current.LinkedName
			break
		}
		m.screen = screenAddProject
		projectName, profileName := m.current.Name, "dev"
		if candidate, exists := m.manifest.Projects[m.current.Name]; exists {
			if _, linked := m.cfg.Projects[m.current.Name]; !linked {
				projectName = m.current.Name
				profiles := sortedKeys(candidate.Profiles)
				if len(profiles) > 0 {
					profileName = profiles[0]
				}
			}
		}
		m.fields = []field{{"Project name", projectName, false}, {"Initial profile", profileName, false}}
		m.fieldCursor = 0
	case "c":
		if len(m.projects) == 0 {
			break
		}
		project := m.projects[m.projectCursor]
		active := m.cfg.Projects[project].ActiveProfile
		if active == "" {
			m.errText = "project has no active profile"
			break
		}
		m.busy = true
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, active) }, "profile captured")
	case "p":
		if !app.HasRemote(*m.cfg) {
			m.errText = "configure a vault sync repository first with g"
			break
		}
		m.busy = true
		return m, opCmd(func() error { return app.Pull(*m.cfg) }, "vault pulled; local .env files unchanged")
	case "u":
		if !app.HasRemote(*m.cfg) {
			m.errText = "configure a vault sync repository first with g"
			break
		}
		m.busy = true
		return m, opCmd(func() error { return app.Push(*m.cfg) }, "vault pushed")
	case "g":
		m.screen = screenRemote
		m.menuCursor = 0
	case "b":
		home, _ := os.UserHomeDir()
		m.screen = screenRecovery
		m.fields = []field{{"Recovery backup path", filepath.Join(home, "gitenv-recovery.txt"), false}}
		m.fieldCursor = 0
	case "r":
		return m, loadCmd(m.cfg, m.cwd)
	}
	return m, nil
}

func (m model) remoteMenuKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := 4
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < items-1 {
			m.menuCursor++
		}
	case "enter":
		switch m.menuCursor {
		case 0: // Change
			m.screen = screenRemoteChange
			m.fields = []field{{"Vault sync repository URL", m.remoteURL, false}}
			m.fieldCursor = 0
		case 1: // Test
			cfg := *m.cfg
			m.busy = true
			return m, opCmd(func() error { return app.TestVaultRemote(cfg) }, "vault sync repository is reachable")
		case 2: // Remove
			m.screen = screenConfirmRemoveRemote
		case 3: // Back
			m.screen = screenProjects
		}
	}
	return m, nil
}

func (m model) unlockMenuKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := 4
	switch key.String() {
	case "esc", "q":
		m.errText = "unlock or disconnect the vault before continuing"
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < items-1 {
			m.menuCursor++
		}
	case "enter":
		switch m.menuCursor {
		case 0: // Unlock with master password
			if m.migrationRecoveryRequired {
				m.errText = "this legacy vault has no master password; use recovery or device approval"
				break
			}
			m.screen = screenUnlockPassword
			m.fields = []field{{"Master password", "", true}}
			m.fieldCursor = 0
		case 1:
			if m.cfg.PendingEnrollmentID != "" {
				cfg := m.cfg
				requestID := cfg.PendingEnrollmentID
				m.busy = true
				return m, opCmd(func() error {
					return app.ActivateDeviceEnrollment(cfg, requestID, vault.StoreIdentityKeychain)
				}, "device enrollment activated; vault is now accessible")
			}
			hostname, _ := os.Hostname()
			m.screen = screenEnrollRequest
			m.fields = []field{{"Device name", hostname, false}}
			m.fieldCursor = 0
		case 2: // Import recovery identity (advanced)
			m.screen = screenImportRecovery
			m.fields = []field{{"Recovery key", "", true}}
			m.fieldCursor = 0
		case 3:
			m.screen = screenConfirmDisconnect
		}
	}
	return m, nil
}

func (m model) profilesKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc":
		m.screen = screenProjects
		m.selectedProject = ""
	case "up", "k":
		if m.profileCursor > 0 {
			m.profileCursor--
		}
	case "down", "j":
		if m.profileCursor < len(m.profiles)-1 {
			m.profileCursor++
		}
	case "enter":
		if len(m.profiles) == 0 {
			break
		}
		profile := m.profiles[m.profileCursor]
		if m.statuses[m.selectedProject] == "modified" || m.statuses[m.selectedProject] == "unmanaged" {
			m.pendingProfile = profile
			m.screen = screenConfirm
			break
		}
		project := m.selectedProject
		m.busy = true
		return m, opCmd(func() error { return vault.Apply(m.cfg, project, profile, false) }, "profile applied")
	case "c":
		active := m.cfg.Projects[m.selectedProject].ActiveProfile
		if active == "" {
			m.errText = "project has no active profile"
			break
		}
		project := m.selectedProject
		m.busy = true
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, active) }, "profile captured")
	case "n":
		m.screen = screenNewProfile
		m.fields = []field{{"New profile name", "", false}}
		m.fieldCursor = 0
	case "d":
		if len(m.profiles) == 0 {
			break
		}
		profile := m.profiles[m.profileCursor]
		if m.cfg.Projects[m.selectedProject].ActiveProfile == profile {
			m.errText = "active profile cannot be removed; apply another profile first"
			break
		}
		m.pendingProfile = profile
		m.screen = screenConfirmDelete
	}
	return m, nil
}

func (m model) confirmKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.screen = screenProfiles
		m.busy = true
		return m, opCmd(func() error {
			return vault.Apply(m.cfg, project, profile, true)
		}, "profile applied; local changes discarded")
	}
	m.screen = screenProfiles
	m.pendingProfile = ""
	m.info = "cancelled"
	return m, nil
}

func (m model) confirmDeleteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		project, profile := m.selectedProject, m.pendingProfile
		m.pendingProfile = ""
		m.screen = screenProfiles
		m.busy = true
		return m, opCmd(func() error { return vault.RemoveProfile(m.cfg, project, profile) }, "profile removed")
	}
	m.screen = screenProfiles
	m.pendingProfile = ""
	m.info = "cancelled"
	return m, nil
}

func (m model) confirmRemoveRemoteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		cfg := *m.cfg
		m.busy = true
		return m, opCmd(func() error { return app.RemoveVaultRemote(cfg) }, "vault sync repository removed")
	}
	m.screen = screenRemote
	m.info = "cancelled"
	return m, nil
}

func (m model) confirmDisconnectKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		m.screen = screenOnboarding
		m.busy = true
		return m, opCmd(func() error { return app.DisconnectVault(m.cfg) }, "vault disconnected from this computer")
	}
	m.screen = screenUnlock
	m.info = "cancelled"
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("gitenv")
	if m.busy {
		b.WriteString(" — working…")
	}
	b.WriteString("\n" + strings.Repeat("─", 64) + "\n")
	switch m.screen {
	case screenOnboarding:
		m.viewOnboarding(&b)
	case screenCreate:
		m.viewForm(&b, "Create a new protected vault")
	case screenClone:
		m.viewForm(&b, "Clone an existing vault")
	case screenAddProject:
		m.viewForm(&b, "Add current project")
	case screenNewProfile:
		m.viewForm(&b, "Capture .env as a new profile")
	case screenRemote:
		m.viewRemoteMenu(&b)
	case screenRemoteChange:
		m.viewForm(&b, "Vault sync repository")
	case screenMigrate:
		m.viewForm(&b, "Migrate vault to protected access")
	case screenUnlock:
		m.viewUnlockMenu(&b)
	case screenUnlockPassword:
		m.viewForm(&b, "Unlock vault")
	case screenEnrollRequest:
		m.viewForm(&b, "Request device approval")
	case screenImportRecovery:
		m.viewForm(&b, "Paste recovery identity")
	case screenRecovery:
		m.viewForm(&b, "Export recovery identity")
	case screenProjects:
		m.viewProjects(&b)
	case screenProfiles:
		m.viewProfiles(&b)
	case screenConfirm:
		fmt.Fprintf(&b, "Local .env has uncaptured content.\nApply %q and discard it? [y/N]\n", m.pendingProfile)
	case screenConfirmDelete:
		fmt.Fprintf(&b, "Remove encrypted profile %q? This cannot be undone. [y/N]\n", m.pendingProfile)
	case screenConfirmRemoveRemote:
		fmt.Fprintln(&b, "Remove vault sync repository? [y/N]")
	case screenConfirmDisconnect:
		fmt.Fprintln(&b, "Disconnect this vault from this computer?\nEncrypted vault files and its remote will not be deleted. [y/N]")
	}
	if m.info != "" {
		b.WriteString("\n✓ " + m.info + "\n")
	}
	if m.errText != "" {
		b.WriteString("\n✗ " + m.errText + "\n")
	}
	return b.String()
}

func (m model) viewOnboarding(b *strings.Builder) {
	b.WriteString("No vault configured. Choose how to begin:\n\n")
	items := []string{"Create a new vault", "Clone an existing vault"}
	for i, item := range items {
		cursor := "  "
		if i == m.menuCursor {
			cursor = "▶ "
		}
		b.WriteString(cursor + item + "\n")
	}
	b.WriteString("\n↑↓ select  enter continue  q quit\n")
}

func (m model) viewForm(b *strings.Builder, title string) {
	b.WriteString(title + "\n\n")
	for i, f := range m.fields {
		cursor := "  "
		if i == m.fieldCursor {
			cursor = "▶ "
		}
		displayValue := f.value
		if f.masked {
			displayValue = strings.Repeat("*", utf8.RuneCountInString(f.value))
		}
		fmt.Fprintf(b, "%s%-28s %s%s\n", cursor, f.label+":", displayValue, inputCursor(i == m.fieldCursor))
	}
	b.WriteString("\nTab next field  Enter confirm  Ctrl+U clear  Esc cancel\n")
}

func (m model) viewRemoteMenu(b *strings.Builder) {
	if m.remoteDisplayURL == "" {
		b.WriteString("Vault sync repository: (none)\n\n")
	} else {
		b.WriteString("Vault sync repository: " + m.remoteDisplayURL + "\n\n")
	}
	items := []string{"Change", "Test", "Remove", "Back"}
	for i, item := range items {
		cursor := "  "
		if i == m.menuCursor {
			cursor = "▶ "
		}
		b.WriteString(cursor + item + "\n")
	}
	b.WriteString("\n↑↓ select  enter confirm  esc back\n")
}

func (m model) viewUnlockMenu(b *strings.Builder) {
	b.WriteString("Vault access is required. Choose an option:\n\n")
	passwordOption := "Unlock with master password"
	if m.migrationRecoveryRequired {
		passwordOption = "Master password unavailable for this legacy vault"
	}
	approvalOption := "Request approval from another device"
	if id := m.cfg.PendingEnrollmentID; id != "" {
		short := id
		if len(short) > 8 {
			short = short[:8] + "…"
		}
		approvalOption = "Check pending device approval (" + short + ")"
	}
	items := []string{
		passwordOption,
		approvalOption,
		"Paste recovery key (advanced)",
		"Disconnect this vault and start again",
	}
	for i, item := range items {
		cursor := "  "
		if i == m.menuCursor {
			cursor = "▶ "
		}
		b.WriteString(cursor + item + "\n")
	}
	b.WriteString("\n↑↓ select  enter confirm  esc back\n")
}

func (m model) viewProjects(b *strings.Builder) {
	fmt.Fprintf(b, "Vault: %s\nCurrent: %s", m.cfg.VaultPath, m.current.Path)
	if m.current.HasEnv {
		b.WriteString("  [.env found]")
	}
	if m.current.LinkedName != "" {
		b.WriteString("  [linked: " + m.current.LinkedName + "]")
	}
	b.WriteString("\n\n")
	if len(m.projects) == 0 {
		b.WriteString("No projects linked on this computer. Press a in a directory with .env.\n")
	}
	for i, name := range m.projects {
		cursor := "  "
		if i == m.projectCursor {
			cursor = "▶ "
		}
		active := m.cfg.Projects[name].ActiveProfile
		if active == "" {
			active = "(none)"
		}
		fmt.Fprintf(b, "%s%-22s %-14s %s\n", cursor, name, active, m.statuses[name])
	}
	b.WriteString("\nenter profiles  a add current  c capture  p vault pull  u vault push\n")
	b.WriteString("g vault sync  b recovery options  r reload  q quit\n")
}

func (m model) viewProfiles(b *strings.Builder) {
	local := m.cfg.Projects[m.selectedProject]
	fmt.Fprintf(b, "%s\nPath: %s\nActive: %s  Status: %s\n\n",
		m.selectedProject, local.Path, local.ActiveProfile, m.statuses[m.selectedProject])
	for i, profile := range m.profiles {
		cursor := "  "
		if i == m.profileCursor {
			cursor = "▶ "
		}
		active := ""
		if profile == local.ActiveProfile {
			active = " ●"
		}
		b.WriteString(cursor + profile + active + "\n")
	}
	b.WriteString("\nenter apply  c capture active  n new profile  d remove  esc back\n")
}

func inputCursor(active bool) string {
	if active {
		return "█"
	}
	return ""
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 160 && strings.Contains(text, "=") {
		return "operation failed (details hidden to protect secrets)"
	}
	return text
}

func Run(cfg *vault.LocalConfig, cwd string) error {
	if cfg == nil {
		return errors.New("gitenv tui: config must not be nil")
	}
	program := tea.NewProgram(newModel(cfg, cwd), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("gitenv tui: %w", err)
	}
	return nil
}
