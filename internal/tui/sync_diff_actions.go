package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
)

type syncDiffTarget struct {
	project string
	profile string
}

func (m model) syncDiffTargets() []syncDiffTarget {
	targets := make([]syncDiffTarget, 0, len(m.syncInventory.LocalEnvs))
	for _, local := range m.syncInventory.LocalEnvs {
		targets = append(targets, syncDiffTarget{project: local.Project, profile: local.Profile})
	}
	return targets
}

func (m model) selectedSyncDiffTarget() (syncDiffTarget, bool) {
	targets := m.syncDiffTargets()
	if len(targets) == 0 {
		return syncDiffTarget{}, false
	}
	index := min(max(0, m.syncDiffSelection), len(targets)-1)
	return targets[index], true
}

func (m model) isSelectedSyncDiff(project, profile string) bool {
	target, ok := m.selectedSyncDiffTarget()
	return ok && target.project == project && target.profile == profile
}

func (m model) selectNextSyncDiff(direction int) model {
	targets := m.syncDiffTargets()
	if len(targets) == 0 {
		m.syncDiffSelection = 0
		return m
	}
	m.syncDiffSelection = (m.syncDiffSelection + direction + len(targets)) % len(targets)
	m.syncDiffOffset = 0
	return m
}

func (m model) requestDiffPublish() (tea.Model, tea.Cmd) {
	target, ok := m.selectedSyncDiffTarget()
	if !ok {
		m.errText = "select a modified local .env to publish"
		return m, nil
	}
	if m.syncStatus.State != gitops.SyncSynced || m.syncStatus.Dirty {
		m.errText = "vault must be synchronized and clean before publishing one environment"
		return m, nil
	}
	m.pendingDiffProject, m.pendingDiffProfile = target.project, target.profile
	m.screen = screenConfirmDiffPublish
	return m, nil
}

func (m model) requestDiffDiscard() (tea.Model, tea.Cmd) {
	target, ok := m.selectedSyncDiffTarget()
	if !ok {
		m.errText = "select a modified local .env to discard"
		return m, nil
	}
	m.pendingDiffProject, m.pendingDiffProfile = target.project, target.profile
	m.screen = screenConfirmDiffDiscard
	return m, nil
}

func (m model) confirmDiffActionKey(key tea.KeyMsg, publish bool) (tea.Model, tea.Cmd) {
	if key.String() != "y" && key.String() != "Y" {
		m.screen = screenSyncDiff
		m.pendingDiffProject, m.pendingDiffProfile = "", ""
		m.info = "cancelled"
		return m, nil
	}
	project, profile := m.pendingDiffProject, m.pendingDiffProfile
	m.pendingDiffProject, m.pendingDiffProfile = "", ""
	m.syncLineDiff = nil
	m.syncDiffOffset = 0
	m.screen = screenProjects
	m.busy = true
	if publish {
		return m, opCmd(func() error { return app.PublishLocalEnv(m.cfg, project, profile) }, fmt.Sprintf("%s/%s captured and published", project, profile))
	}
	return m, opCmd(func() error { return app.DiscardLocalEnv(m.cfg, project, profile) }, fmt.Sprintf("%s/%s restored from active profile", project, profile))
}
