package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/envdiff"
	"github.com/eaedave/gitenv/internal/vault"
)

func (m model) requestCapturePreview(project, profile string, intent captureIntent) (tea.Model, tea.Cmd) {
	if err := vault.ValidateName("project", project); err != nil {
		m.busy = false
		m.errText = err.Error()
		return m, nil
	}
	if err := vault.ValidateName("profile", profile); err != nil {
		m.busy = false
		m.errText = err.Error()
		return m, nil
	}
	projectPath, err := m.captureProjectPath(project, intent)
	if err != nil {
		m.busy = false
		m.errText = err.Error()
		return m, nil
	}
	m.busy = true
	return m, capturePreviewCmd(m.cfg, projectPath, project, profile, intent)
}

func (m model) captureProjectPath(project string, intent captureIntent) (string, error) {
	if intent == captureNewProject {
		if m.current.Path == "" {
			return "", fmt.Errorf("current project path is unavailable")
		}
		return m.current.Path, nil
	}
	local, ok := m.cfg.Projects[project]
	if !ok {
		return "", fmt.Errorf("project %q is not linked on this computer", project)
	}
	return local.Path, nil
}

func capturePreviewCmd(cfg *vault.LocalConfig, projectPath, project, profile string, intent captureIntent) tea.Cmd {
	return func() tea.Msg {
		current, err := os.ReadFile(filepath.Join(projectPath, ".env"))
		if err != nil {
			return capturePreviewMsg{err: fmt.Errorf("read project .env: %w", err)}
		}
		base, err := capturePreviewBase(cfg, project, profile)
		if err != nil {
			return capturePreviewMsg{err: err}
		}
		return capturePreviewMsg{
			diff:    envdiff.Compare(base, current),
			project: project,
			profile: profile,
			intent:  intent,
		}
	}
}

func capturePreviewBase(cfg *vault.LocalConfig, project, profile string) ([]byte, error) {
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return nil, err
	}
	projectEntry, exists := manifest.Projects[project]
	if !exists {
		return nil, nil
	}
	if _, exists := projectEntry.Profiles[profile]; !exists {
		return nil, nil
	}
	return vault.ReadProfile(cfg, project, profile)
}

func (m model) confirmCaptureKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() != "y" && key.String() != "Y" {
		return m.cancelCapturePreview()
	}
	project, profile, intent := m.pendingProject, m.pendingProfile, m.pendingCapture
	m.clearCapturePreview()
	if intent == captureNewProject || m.selectedProject == "" {
		m.screen = screenProjects
	} else {
		m.screen = screenProfiles
	}
	m.busy = true
	switch intent {
	case captureNewProject:
		return m, opCmd(func() error {
			return app.AddCurrentProject(m.cfg, m.current, project, profile)
		}, "new project added and .env captured")
	case captureNewProfile:
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, profile) }, "new profile captured")
	default:
		return m, opCmd(func() error { return vault.Capture(m.cfg, project, profile) }, "profile captured")
	}
}

func (m model) cancelCapturePreview() (tea.Model, tea.Cmd) {
	intent := m.pendingCapture
	m.clearCapturePreview()
	m.info = "cancelled"
	if intent == captureNewProject || m.selectedProject == "" {
		m.screen = screenProjects
	} else {
		m.screen = screenProfiles
	}
	return m, nil
}

func (m *model) clearCapturePreview() {
	m.pendingProject = ""
	m.pendingProfile = ""
	m.pendingCapture = captureExistingProfile
	m.captureDiff = envdiff.Diff{}
}
