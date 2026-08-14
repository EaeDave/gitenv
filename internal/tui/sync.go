package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
)

func (m model) requestContextualSync() (tea.Model, tea.Cmd) {
	switch m.syncStatus.State {
	case gitops.SyncChecking:
		m.errText = "remote status is still checking; try again shortly"
	case gitops.SyncSynced:
		// "Already synchronized" used to be a dead end. Offering the explicit
		// pull and push here replaces the undocumented `p` and `u` keys, one of
		// which sat a slipped shift away from the self-update key.
		m.screen, m.menuCursor = screenSyncActions, 0
	case gitops.SyncLocalAhead, gitops.SyncRemoteAhead:
		m.pendingSync = m.syncStatus.State
		m.screen = screenConfirmSync
	case gitops.SyncNoRemote:
		m.errText = "no sync repository configured; press g to configure one"
	case gitops.SyncDiverged:
		// A diverged vault used to end here with "automatic sync is blocked" and
		// no way forward inside the app. Now it opens the resolution screen.
		m.busy = true
		return m, divergedCmd(m.cfg)
	case gitops.SyncOffline:
		m.errText = "remote is unreachable; check the connection and press r to retry"
	case gitops.SyncAuthError:
		m.errText = "Git authentication failed; verify credentials and press r to retry"
	default:
		m.errText = "remote status could not be determined; press r to retry"
	}
	return m, nil
}

// syncActionsKey drives the explicit pull/push menu shown for a vault that is
// already in sync.
func (m model) syncActionsKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	target := screenProjects
	if m.selectedProject != "" {
		target = screenProfiles
	}
	switch key.String() {
	case "esc", "q":
		m.screen = target
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(2, m.menuCursor+1)
	case "enter":
		switch m.menuCursor {
		case 0:
			m.screen, m.busy = target, true
			return m, opCmd(func() error { return app.Pull(*m.cfg) }, "vault up to date; local env files unchanged")
		case 1:
			m.screen, m.busy = target, true
			return m, opCmd(func() error { return app.Push(*m.cfg) }, "vault changes published")
		default:
			m.screen = target
		}
	}
	return m, nil
}

func (m model) confirmSyncKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
