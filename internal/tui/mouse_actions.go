package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const mouseDoubleClickWindow = 400 * time.Millisecond

func (m model) handleMouseInteraction(msg mouseInteractionMsg) (tea.Model, tea.Cmd) {
	switch msg.kind {
	case mouseEventHover:
		m.hoveredMouseTarget = msg.target
		return m, nil
	case mouseEventRelease:
		return m, nil
	case mouseEventWheel:
		return m.handleMouseWheel(msg)
	case mouseEventClick:
		if msg.button != tea.MouseLeft {
			return m, nil
		}
		m.hoveredMouseTarget = msg.target
		return m.handleMouseClick(msg)
	default:
		return m, nil
	}
}

func (m model) handleMouseWheel(msg mouseInteractionMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenProjects && m.projectList != nil &&
		(msg.target.kind == mouseTargetProjectList || msg.target.kind == mouseTargetProjectRow) {
		for range 3 {
			switch msg.button {
			case tea.MouseWheelUp:
				m.projectList.CursorUp()
			case tea.MouseWheelDown:
				m.projectList.CursorDown()
			default:
				return m, nil
			}
		}
		m.syncProjectCursor()
		return m, nil
	}
	if m.screen == screenProfiles && msg.target.kind == mouseTargetProfileRow {
		switch msg.button {
		case tea.MouseWheelUp:
			m.profileCursor = max(0, m.profileCursor-1)
		case tea.MouseWheelDown:
			m.profileCursor = min(max(0, len(m.profiles)-1), m.profileCursor+1)
		}
	}
	return m, nil
}

func (m model) handleMouseClick(msg mouseInteractionMsg) (tea.Model, tea.Cmd) {
	switch msg.target.kind {
	case mouseTargetProjectRow:
		if m.projectList == nil || msg.target.index < 0 || msg.target.index >= len(m.projectList.VisibleItems()) {
			return m, nil
		}
		m.projectList.Select(msg.target.index)
		m.syncProjectCursor()
		if m.isDoubleClick(msg) {
			m.resetDoubleClick()
			m.openSelectedProject()
			m.clearMouseFeedback()
			return m, nil
		}
		m.rememberClick(msg)
		return m, nil
	case mouseTargetProfileRow:
		if msg.target.index < 0 || msg.target.index >= len(m.profiles) {
			return m, nil
		}
		m.profileCursor = msg.target.index
		if m.isDoubleClick(msg) {
			m.resetDoubleClick()
			m.clearMouseFeedback()
			return m.applySelectedProfile()
		}
		m.rememberClick(msg)
		return m, nil
	case mouseTargetButton:
		// Bubble Tea defines MouseClickMsg as the portable activation event;
		// release events are not available in every terminal fallback mode.
		return m.handleMouseButton(msg.target.action)
	case mouseTargetField:
		if msg.target.index >= 0 && msg.target.index < len(m.fields) {
			m.fieldCursor = msg.target.index
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) isDoubleClick(msg mouseInteractionMsg) bool {
	elapsed := msg.at.Sub(m.lastMouseClickAt)
	return !m.lastMouseClickAt.IsZero() && msg.target == m.lastClickedMouseTarget && elapsed >= 0 && elapsed <= mouseDoubleClickWindow
}

func (m *model) rememberClick(msg mouseInteractionMsg) {
	m.lastClickedMouseTarget = msg.target
	m.lastMouseClickAt = msg.at
}

func (m *model) resetDoubleClick() {
	m.lastClickedMouseTarget = mouseTarget{}
	m.lastMouseClickAt = time.Time{}
}

func (m model) handleMouseButton(action mouseAction) (tea.Model, tea.Cmd) {
	m.hoveredMouseTarget = mouseTarget{}
	switch action {
	case mouseActionOpenProject:
		m.openSelectedProject()
		return m, nil
	case mouseActionCaptureProject:
		return m.captureSelectedProject()
	case mouseActionProjectOptions:
		m.openProjectOptions()
		return m, nil
	case mouseActionApplyProfile:
		return m.applySelectedProfile()
	case mouseActionEditProfile:
		return m.openEditor(m.selectedProject, screenProfiles)
	case mouseActionCaptureProfile:
		return m.captureActiveProfile()
	case mouseActionProfileOptions:
		m.openProjectOptionsFor(m.selectedProject)
		return m, nil
	case mouseActionSync:
		return m.requestContextualSync()
	case mouseActionReviewChanges:
		return m.handleKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	case mouseActionEditCapture:
		return m.openCaptureEditor()
	case mouseActionConfirm:
		return m.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	case mouseActionCancel:
		return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	case mouseActionContinue:
		return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	default:
		return m, nil
	}
}

func (m *model) clearMouseFeedback() {
	m.hoveredMouseTarget = mouseTarget{}
}
