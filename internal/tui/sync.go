package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
)

func (m model) requestContextualSync() (tea.Model, tea.Cmd) {
	switch m.syncStatus.State {
	case gitops.SyncChecking:
		m.errText = "remote status is still checking; try again shortly"
	case gitops.SyncSynced:
		m.info = "vault is already synchronized"
	case gitops.SyncLocalAhead, gitops.SyncRemoteAhead:
		m.pendingSync = m.syncStatus.State
		m.screen = screenConfirmSync
	case gitops.SyncNoRemote:
		m.errText = "no sync repository configured; press g to configure one"
	case gitops.SyncDiverged:
		m.errText = "local and remote vault changed independently; automatic sync is blocked"
	case gitops.SyncOffline:
		m.errText = "remote is unreachable; check the connection and press r to retry"
	case gitops.SyncAuthError:
		m.errText = "Git authentication failed; verify credentials and press r to retry"
	default:
		m.errText = "remote status could not be determined; press r to retry"
	}
	return m, nil
}

func (m model) confirmSyncKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	targetScreen := screenProjects
	if m.selectedProject != "" {
		targetScreen = screenProfiles
	}
	if key.String() != "y" && key.String() != "Y" {
		m.screen, m.pendingSync, m.info = targetScreen, "", "cancelled"
		return m, nil
	}
	pending := m.pendingSync
	m.screen, m.pendingSync, m.busy = targetScreen, "", true
	switch pending {
	case gitops.SyncRemoteAhead:
		return m, opCmd(func() error { return app.Pull(*m.cfg) }, "vault synchronized; local .env files unchanged")
	case gitops.SyncLocalAhead:
		if m.syncStatus.Dirty {
			return m, opCmd(func() error { return app.Push(*m.cfg) }, "vault changes published")
		}
		return m, opCmd(func() error { return app.PushExisting(*m.cfg) }, "vault commits published")
	default:
		m.busy = false
		m.errText = "sync state changed; press r to check again"
		return m, nil
	}
}
