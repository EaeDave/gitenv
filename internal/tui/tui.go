// Package tui provides gitenv's primary terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/envdiff"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/update"
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
	screenAdoptClone          // form: destination directory for clone-and-adopt
	screenAdoptLink           // form: local project directory to link
	screenAdoptCandidates     // cursor menu: pick one of several discovered clones
	screenAdoptProfile        // cursor menu: choose existing profile before clone/link
	screenProjectOptions      // form: env file + line endings
	screenRemote              // cursor menu: Change / Test / Remove / Back
	screenRemoteChange        // form: vault sync repository URL
	screenConfirmRemoveRemote // y/N: remove vault sync repository
	screenMigrate             // form: password / confirm / device — protect unprotected vault
	screenUnlock              // cursor menu: password / enrollment / import-recovery
	screenUnlockPassword      // form: master password (masked)
	screenEnrollRequest       // form: device name → RequestDeviceEnrollment
	screenImportRecovery      // form: pasted recovery identity → ImportIdentityValue
	screenRecovery            // form: export recovery identity (b key)
	screenRecoveryPrompt      // form: export recovery identity, right after vault creation
	screenDevices             // cursor menu: enrolled devices + pending approvals
	screenConfirmApprove      // y/N: approve a pending device enrollment
	screenConfirmReject       // y/N: reject and remove a pending enrollment
	screenDiverged            // cursor menu: how to resolve a diverged vault
	screenDivergedProfiles    // cursor menu: per-profile choice for both-changed profiles
	screenConfirmDiverged     // y/N: apply the chosen divergence resolution
	screenSyncActions         // cursor menu: explicit pull / push on a synced vault
	screenHelp                // full keymap + glossary for the originating screen
	screenConfirmDisconnect
	screenConfirm
	screenConfirmDelete
	screenConfirmSync
	screenConfirmCapture
	screenSyncDiff
	screenConfirmDiffPublish
	screenConfirmDiffDiscard
	screenEditor
	screenConfirmEditorDiscard
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
	profileStatuses          map[string]map[string]string
	current                  app.CurrentProject
	remoteURL                string // raw URL, only when safe to prefill (no credentials)
	remoteDisplayURL         string // redacted URL for display
	needsMigration           bool
	migrationIdentityMissing bool
	needsUnlock              bool
	upgraded                 bool // vault metadata was upgraded to v3 this load
	recoveryExported         bool // a recovery key has been exported from this computer
}

type syncStatusMsg struct {
	status    gitops.SyncStatus
	inventory app.SyncInventory
}
type syncLineDiffMsg struct {
	diff app.SyncLineDiff
	err  error
}

type captureIntent int

const (
	captureExistingProfile captureIntent = iota
	captureNewProfile
	captureNewProject
)

type capturePreviewMsg struct {
	diff    envdiff.Diff
	project string
	profile string
	intent  captureIntent
	err     error
}

type updateCheckMsg struct {
	latest    string
	available bool
}

type updateAppliedMsg struct {
	path string
	err  error
}

type model struct {
	cfg                                                    *vault.LocalConfig
	cwd                                                    string
	current                                                app.CurrentProject
	manifest                                               vault.Manifest
	statuses                                               map[string]string
	profileStatuses                                        map[string]map[string]string
	projects, profiles                                     []string
	projectStates                                          []app.ProjectState
	projectList                                            *list.Model
	adoptName, adoptPath                                   string
	adoptCandidates, adoptProfiles                         []string
	adoptReturn                                            screen
	projectCursor, profileCursor, menuCursor, fieldCursor  int
	selectedProject, pendingProfile, pendingProject        string
	openProjectAfterReload                                 string
	pendingSync                                            gitops.SyncState
	pendingCapture                                         captureIntent
	captureDiff                                            envdiff.Diff
	screen                                                 screen
	fields                                                 []field
	info, errText                                          string
	busy                                                   bool
	remoteURL                                              string // safe prefill URL (no embedded credentials)
	remoteDisplayURL                                       string // redacted display URL
	accessRequired                                         bool
	migrationRecoveryRequired                              bool
	browseProjects, landed                                 bool
	syncStatus                                             gitops.SyncStatus
	syncInventory                                          app.SyncInventory
	syncDiffOffset                                         int
	syncLineDiff                                           *app.SyncLineDiff
	syncDiffLoading                                        bool
	width, height                                          int
	isDark                                                 bool
	syncDiffSelection                                      int
	syncDiffReturn                                         screen
	pendingDiffProject, pendingDiffProfile                 string
	spinner                                                spinner.Model
	editor                                                 textarea.Model
	editorRaw                                              []byte
	editorBase                                             []byte
	editorProject, editorBaseProfile                       string
	editorCRLF, editorTrailingNewline, editorBaseAvailable bool
	editorReturn                                           screen
	version                                                string
	updateLatest                                           string
	updateAvailable, updating                              bool
	pendingRestart                                         string
	// pendingApprovals holds device enrollment requests waiting in the vault,
	// excluding this computer's own. Populated on every reload so the projects
	// screen can say a device is waiting, instead of the request sitting
	// invisible in the manifest until somebody reads it by hand.
	pendingApprovals []vault.EnrollmentRequest
	approvalCursor   int
	// Divergence resolution state, built when the user opens screenDiverged.
	diverged        *app.DivergenceReport
	divergedChoices map[string]app.DivergenceChoice
	divergedCursor  int
	// recoveryExported records whether a recovery key was ever exported from
	// this computer, so the header keeps warning until it has been.
	recoveryExported bool
	// helpReturn is the screen the in-app help was opened from; optionsReturn
	// likewise for the project options form, which is now reachable from both
	// the project list and a project's profiles screen.
	helpReturn, optionsReturn screen
	helpOffset                int
}

func newModel(cfg *vault.LocalConfig, cwd, version string) model {
	activity := spinner.New()
	activity.Spinner = spinner.Dot
	activity.Style = styles.warning
	m := model{cfg: cfg, cwd: cwd, version: version, statuses: map[string]string{}, spinner: activity, syncStatus: gitops.SyncStatus{State: gitops.SyncChecking}, isDark: true}
	current, err := app.DetectCurrent(*cfg, cwd)
	if err == nil {
		m.current = current
	}
	switch {
	case cfg.VaultPath == "":
		m.screen = screenOnboarding
	case m.current.LinkedName != "":
		m.screen = screenProfiles
		m.selectedProject = m.current.LinkedName
	default:
		m.screen = screenProjects
	}
	m.projectList = newProjectList(m.projectListItems(), m.projectListWidth(), m.projectListHeight(), m.isDark)
	return m
}

// syncRefreshInterval is how often the sync panel re-checks the remote on its
// own. Without this the panel showed whatever it found at launch until the user
// pressed `r`, and a long-lived session quietly displayed stale information.
const syncRefreshInterval = 90 * time.Second

type syncRefreshTickMsg struct{}

// syncRefreshTick schedules the next background sync check. The tick always
// reschedules; whether it actually inspects the remote is decided on arrival, so
// a busy or locked TUI never queues work it cannot use.
func syncRefreshTick() tea.Cmd {
	return tea.Tick(syncRefreshInterval, func(time.Time) tea.Msg { return syncRefreshTickMsg{} })
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, syncRefreshTick(), tea.RequestBackgroundColor}
	if !update.Disabled() && update.IsRelease(m.version) {
		cmds = append(cmds, checkUpdateCmd(m.version))
	}
	if m.cfg.VaultPath != "" {
		cmds = append(cmds, loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
	}
	return tea.Batch(cmds...)
}

func checkUpdateCmd(current string) tea.Cmd {
	return func() tea.Msg {
		latest, newer, err := update.Check(context.Background(), current)
		if err != nil {
			return updateCheckMsg{}
		}
		return updateCheckMsg{latest: latest, available: newer}
	}
}

func applyUpdateCmd(tag string) tea.Cmd {
	return func() tea.Msg {
		path, err := update.Apply(context.Background(), tag)
		return updateAppliedMsg{path: path, err: err}
	}
}

// canAutoUpdate reports whether it is safe to update without disrupting the
// user: only on an idle landing screen, never mid-form or mid-operation.
func (m model) canAutoUpdate() bool {
	if m.busy || m.updating || m.pendingRestart != "" {
		return false
	}
	switch m.screen {
	case screenOnboarding, screenProjects, screenProfiles:
		return true
	default:
		return false
	}
}

func (m model) beginSelfUpdate() (tea.Model, tea.Cmd) {
	if m.updateLatest == "" || m.updating {
		return m, nil
	}
	m.busy = true
	m.updating = true
	m.errText = ""
	m.info = "updating to " + m.updateLatest + "…"
	return m, applyUpdateCmd(m.updateLatest)
}

func loadCmd(cfg *vault.LocalConfig, cwd string) tea.Cmd {
	return func() tea.Msg {
		upgraded, err := app.EnsureVaultUpgraded(*cfg)
		if err != nil {
			return operationMsg{err: err}
		}
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
				return reloadMsg{manifest: manifest, current: current, migrationIdentityMissing: true, upgraded: upgraded}
			}
			return reloadMsg{manifest: manifest, current: current, needsMigration: true, upgraded: upgraded}
		}
		if !identityAllowed {
			return reloadMsg{manifest: manifest, current: current, needsUnlock: true, upgraded: upgraded}
		}
		if current.LinkedName == "" {
			if matches := app.MatchVaultProjects(manifest, current); len(matches) > 0 {
				match := matches[0]
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
		profileStatuses := make(map[string]map[string]string, len(cfg.Projects))
		for name := range cfg.Projects {
			status, statusErr := vault.Status(*cfg, name)
			if statusErr != nil {
				statuses[name] = "error"
			} else {
				statuses[name] = status
			}
			if perProfile, profileErr := vault.ProfileStatuses(*cfg, name); profileErr == nil {
				profileStatuses[name] = perProfile
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
			profileStatuses:  profileStatuses,
			current:          current,
			remoteURL:        prefillURL,
			remoteDisplayURL: displayURL,
			upgraded:         upgraded,
		}
	}
}

func inspectSyncCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		status, inventory := app.InspectSyncWithInventory(*cfg)
		return syncStatusMsg{status: status, inventory: inventory}
	}
}

func revealSyncDiffCmd(cfg *vault.LocalConfig, status gitops.SyncStatus, scope string) tea.Cmd {
	return func() tea.Msg {
		diff, err := app.RevealSyncLineDiff(*cfg, status, scope)
		return syncLineDiffMsg{diff: diff, err: err}
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
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		applyTheme(m.isDark)
		m.spinner.Style = styles.warning
		if m.projectList != nil {
			m.projectList.Styles = list.DefaultStyles(m.isDark)
		}
		if m.screen == screenEditor {
			m.editor.SetStyles(textarea.DefaultStyles(m.isDark))
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeProjectList()
		m = m.applyEditorSize()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case syncStatusMsg:
		m.syncStatus, m.syncInventory = msg.status, msg.inventory
		m.resizeProjectList()
		return m, nil

	case syncRefreshTickMsg:
		// Re-arm unconditionally, then only inspect when a check can be used:
		// an idle, unlocked landing screen. Refreshing mid-form or mid-operation
		// would move the panel under the user or fight a running git command.
		next := syncRefreshTick()
		if m.busy || m.updating || m.accessRequired || m.cfg.VaultPath == "" {
			return m, next
		}
		switch m.screen {
		case screenProjects, screenProfiles:
			return m, tea.Batch(next, inspectSyncCmd(m.cfg))
		default:
			return m, next
		}

	case divergenceReportMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = safeError(msg.err)
			return m, nil
		}
		m.diverged = msg.report
		m.divergedChoices = map[string]app.DivergenceChoice{}
		m.divergedCursor = 0
		m.menuCursor = 0
		m.screen = screenDiverged
		return m, nil

	case updateCheckMsg:
		if msg.available {
			m.updateLatest = msg.latest
			m.updateAvailable = true
			if m.canAutoUpdate() {
				return m.beginSelfUpdate()
			}
		}
		return m, nil

	case updateAppliedMsg:
		m.busy = false
		m.updating = false
		if msg.err != nil {
			m.updateAvailable = false
			m.errText = "update failed: " + safeError(msg.err)
			return m, nil
		}
		m.pendingRestart = msg.path
		return m, tea.Quit

	case syncLineDiffMsg:
		m.busy = false
		m.syncDiffLoading = false
		if msg.err != nil {
			m.syncLineDiff = nil
			m.errText = "could not reveal environment values"
			return m, nil
		}
		m.syncLineDiff = &msg.diff
		m.syncDiffOffset = 0
		return m, nil

	case capturePreviewMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = safeError(msg.err)
			return m, nil
		}
		m.captureDiff = msg.diff
		m.pendingProject = msg.project
		m.pendingProfile = msg.profile
		m.pendingCapture = msg.intent
		m.fields = nil
		m.screen = screenConfirmCapture
		return m, nil

	case discoveryMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = safeError(msg.err)
			return m, nil
		}
		found := 0
		for _, paths := range msg.found {
			found += len(paths)
		}
		m.info = fmt.Sprintf("scan complete — found %d local %s", found, pluralize(found, "repository", "repositories"))
		return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))

	case adoptMsg:
		m.busy = false
		m.screen = screenProjects
		if msg.err != nil {
			if msg.outcome.Method != "" {
				// The clone landed but adopting (link + apply) did not finish. The
				// link may already be persisted, so still reload; tell the user the
				// repository is on disk and its env file needs attention.
				m.errText = fmt.Sprintf("cloned %s via %s into %s, but adopting did not finish: %s — the repository is on disk; open it to resolve its env file", msg.project, msg.outcome.Method, m.adoptPath, safeError(msg.err))
			} else {
				m.errText = safeError(msg.err)
			}
			return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
		}
		if msg.outcome.Method != "" {
			m.info = fmt.Sprintf("adopted %s — cloned via %s into %s", msg.project, msg.outcome.Method, m.adoptPath)
		} else {
			m.info = fmt.Sprintf("adopted %s — linked %s", msg.project, m.adoptPath)
		}
		return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))

	case reloadMsg:
		m.busy = false
		if msg.upgraded {
			m.info = "vault upgraded to v3 — press s to publish the change"
		}
		m.manifest = msg.manifest
		m.statuses = msg.statuses
		m.profileStatuses = msg.profileStatuses
		m.current = msg.current
		m.projectStates = app.ProjectStates(*m.cfg, m.manifest, m.statuses)
		m.projects = projectNames(m.projectStates)
		m.refreshProjectList()
		if m.projectCursor >= len(m.projects) {
			m.projectCursor = max(0, len(m.projects)-1)
		}
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
		if m.openProjectAfterReload != "" {
			if project, ok := m.manifest.Projects[m.openProjectAfterReload]; ok {
				m.selectedProject = m.openProjectAfterReload
				m.profiles = sortedKeys(project.Profiles)
				m.profileCursor = 0
				m.screen = screenProfiles
				m.openProjectAfterReload = ""
			}
		}
		m.pendingApprovals = app.PendingApprovals(*m.cfg, m.manifest)
		if m.approvalCursor >= len(m.pendingApprovals) {
			m.approvalCursor = max(0, len(m.pendingApprovals)-1)
		}
		m.recoveryExported = app.HasRecoveryBackup(*m.cfg)
		if !m.landed {
			m.landed = true
			if !m.browseProjects && m.current.LinkedName != "" {
				m.selectedProject = m.current.LinkedName
				m.profiles = sortedKeys(m.manifest.Projects[m.selectedProject].Profiles)
				m.profileCursor = 0
				m.screen = screenProfiles
			}
		}
		return m, nil

	case operationMsg:
		m.busy = false
		if msg.err != nil {
			m.openProjectAfterReload = ""
			m.errText = safeError(msg.err)
			return m, nil
		}
		m.info = msg.info
		if m.cfg.VaultPath != "" {
			switch m.screen {
			case screenCreate:
				// A brand-new vault has exactly one copy of its key, on this
				// computer. Ask for the backup now, while it matters, instead of
				// hiding it behind an undocumented key the user never finds.
				home, _ := os.UserHomeDir()
				m.screen = screenRecoveryPrompt
				m.fields = []field{{"Recovery key file", filepath.Join(home, "gitenv-recovery.txt"), false}}
				m.fieldCursor = 0
				return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
			case screenClone,
				screenAddProject, screenNewProfile,
				screenRemoteChange, screenConfirmRemoveRemote,
				screenMigrate, screenUnlock, screenUnlockPassword,
				screenImportRecovery, screenRecovery, screenRecoveryPrompt,
				screenDevices, screenConfirmApprove:
				m.screen = screenProjects
			case screenProjectOptions:
				m.screen = m.optionsReturnScreen()
			case screenEnrollRequest:
				m.screen = screenUnlock
			}
			return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
		}

	case tea.KeyPressMsg:
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
	if m.screen == screenEditor {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}
	if m.screen == screenProjects && m.projectList != nil {
		updated, cmd := m.projectList.Update(msg)
		*m.projectList = updated
		m.syncProjectCursor()
		return m, cmd
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

func Run(cfg *vault.LocalConfig, cwd, version string) (string, error) {
	if cfg == nil {
		return "", errors.New("gitenv tui: config must not be nil")
	}
	program := tea.NewProgram(newModel(cfg, cwd, version))
	final, err := program.Run()
	if err != nil {
		return "", fmt.Errorf("gitenv tui: %w", err)
	}
	if m, ok := final.(model); ok {
		return m.pendingRestart, nil
	}
	return "", nil
}
