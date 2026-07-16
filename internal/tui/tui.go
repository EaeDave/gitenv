// Package tui provides gitenv's primary terminal interface.
package tui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
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
	screenConfirmSync
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

type syncStatusMsg struct {
	status gitops.SyncStatus
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
	pendingSync                                           gitops.SyncState
	screen                                                screen
	fields                                                []field
	info, errText                                         string
	busy                                                  bool
	remoteURL                                             string // safe prefill URL (no embedded credentials)
	remoteDisplayURL                                      string // redacted display URL
	accessRequired                                        bool
	migrationRecoveryRequired                             bool
	syncStatus                                            gitops.SyncStatus
	width, height                                         int
	spinner                                               spinner.Model
}

func newModel(cfg *vault.LocalConfig, cwd string) model {
	activity := spinner.New()
	activity.Spinner = spinner.Dot
	activity.Style = styles.warning
	m := model{cfg: cfg, cwd: cwd, statuses: map[string]string{}, spinner: activity, syncStatus: gitops.SyncStatus{State: gitops.SyncChecking}}
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
		return m.spinner.Tick
	}
	return tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg), m.spinner.Tick)
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

func inspectSyncCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		return syncStatusMsg{status: app.InspectSync(*cfg)}
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case syncStatusMsg:
		m.syncStatus = msg.status
		return m, nil

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
			return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
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
