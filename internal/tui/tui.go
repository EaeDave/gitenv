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
	screenRemote
	screenRecovery
	screenConfirm
	screenConfirmDelete
)

type field struct{ label, value string }
type operationMsg struct {
	info string
	err  error
}
type reloadMsg struct {
	manifest vault.Manifest
	statuses map[string]string
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
	return loadCmd(m.cfg)
}

func loadCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		manifest, err := vault.LoadManifest(cfg.VaultPath)
		if err != nil {
			return operationMsg{err: err}
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
		return reloadMsg{manifest: manifest, statuses: statuses}
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
		if m.selectedProject != "" {
			m.profiles = sortedKeys(m.manifest.Projects[m.selectedProject].Profiles)
			if m.profileCursor >= len(m.profiles) {
				m.profileCursor = max(0, len(m.profiles)-1)
			}
		}
		return m, nil
	case operationMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = safeError(msg.err)
			return m, nil
		}
		m.info = msg.info
		if m.cfg.VaultPath != "" {
			if m.screen == screenCreate || m.screen == screenClone {
				m.screen = screenProjects
			}
			if m.screen == screenAddProject || m.screen == screenNewProfile || m.screen == screenRemote || m.screen == screenRecovery {
				m.screen = screenProjects
			}
			current, _ := app.DetectCurrent(*m.cfg, m.cwd)
			m.current = current
			return m, loadCmd(m.cfg)
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
	case screenCreate, screenClone, screenAddProject, screenNewProfile, screenRemote, screenRecovery:
		return m.formKey(key)
	case screenProjects:
		return m.projectsKey(key)
	case screenProfiles:
		return m.profilesKey(key)
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
		if m.menuCursor == 0 {
			configDir, _ := vault.ConfigDir()
			home, _ := os.UserHomeDir()
			m.screen = screenCreate
			m.fields = []field{{"Vault directory", filepath.Join(configDir, "vault")}, {"Recovery backup", filepath.Join(home, "gitenv-recovery.txt")}, {"Git remote (optional)", ""}}
		} else {
			configDir, _ := vault.ConfigDir()
			m.screen = screenClone
			m.fields = []field{{"Git remote URL", ""}, {"Vault directory", filepath.Join(configDir, "vault")}, {"Recovery identity", ""}}
		}
		m.fieldCursor = 0
	}
	return m, nil
}

func (m model) formKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		if m.cfg.VaultPath == "" {
			m.screen = screenOnboarding
		} else if m.selectedProject != "" {
			m.screen = screenProfiles
		} else {
			m.screen = screenProjects
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
		return m, opCmd(func() error { return app.CreateVault(m.cfg, values[0], values[1], values[2]) }, "vault created; recovery identity exported")
	case screenClone:
		return m, opCmd(func() error { return app.CloneVault(m.cfg, values[0], values[1], values[2]) }, "vault cloned and unlocked")
	case screenAddProject:
		if _, exists := m.manifest.Projects[values[0]]; exists {
			return m, opCmd(func() error { return app.LinkExistingProject(m.cfg, m.current, values[0], values[1]) }, "existing project linked and profile applied")
		}
		return m, opCmd(func() error { return app.AddCurrentProject(m.cfg, m.current, values[0], values[1]) }, "new project added and .env captured")
	case screenNewProfile:
		project := m.selectedProject
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, values[0]) }, "new profile captured")
	case screenRemote:
		return m, opCmd(func() error { return app.AddRemote(*m.cfg, values[0]) }, "Git remote configured")
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
		for _, candidate := range sortedKeys(m.manifest.Projects) {
			if _, linked := m.cfg.Projects[candidate]; linked {
				continue
			}
			projectName = candidate
			profiles := sortedKeys(m.manifest.Projects[candidate].Profiles)
			if len(profiles) > 0 {
				profileName = profiles[0]
			}
			break
		}
		m.fields = []field{{"Project name", projectName}, {"Initial profile", profileName}}
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
			m.errText = "configure a Git remote first with g"
			break
		}
		m.busy = true
		return m, opCmd(func() error { return app.Pull(*m.cfg) }, "vault pulled; local .env files unchanged")
	case "u":
		if !app.HasRemote(*m.cfg) {
			m.errText = "configure a Git remote first with g"
			break
		}
		m.busy = true
		return m, opCmd(func() error { return app.Push(*m.cfg) }, "vault pushed")
	case "g":
		if app.HasRemote(*m.cfg) {
			m.errText = "origin remote is already configured"
			break
		}
		m.screen = screenRemote
		m.fields = []field{{"Git remote URL", ""}}
		m.fieldCursor = 0
	case "b":
		home, _ := os.UserHomeDir()
		m.screen = screenRecovery
		m.fields = []field{{"Recovery backup path", filepath.Join(home, "gitenv-recovery.txt")}}
		m.fieldCursor = 0
	case "r":
		return m, loadCmd(m.cfg)
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
		m.fields = []field{{"New profile name", ""}}
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
		return m, opCmd(func() error { return vault.Apply(m.cfg, project, profile, true) }, "profile applied; local changes discarded")
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
		m.viewForm(&b, "Create a new encrypted vault")
	case screenClone:
		m.viewForm(&b, "Clone an existing vault")
	case screenAddProject:
		m.viewForm(&b, "Add current project")
	case screenNewProfile:
		m.viewForm(&b, "Capture .env as a new profile")
	case screenRemote:
		m.viewForm(&b, "Configure Git remote")
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
		fmt.Fprintf(b, "%s%-24s %s%s\n", cursor, f.label+":", f.value, inputCursor(i == m.fieldCursor))
	}
	b.WriteString("\nTab next field  Enter confirm  Ctrl+U clear  Esc cancel\n")
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
	b.WriteString("\nenter profiles  a add current  c capture  p pull  u push\n")
	b.WriteString("g remote  b recovery backup  r reload  q quit\n")
}

func (m model) viewProfiles(b *strings.Builder) {
	local := m.cfg.Projects[m.selectedProject]
	fmt.Fprintf(b, "%s\nPath: %s\nActive: %s  Status: %s\n\n", m.selectedProject, local.Path, local.ActiveProfile, m.statuses[m.selectedProject])
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
