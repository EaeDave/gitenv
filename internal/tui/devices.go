package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

// devicesKey drives the device roster. The cursor lands only on pending
// requests; enrolled devices are listed for reassurance, not mutation.
func (m model) devicesKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		m.approvalCursor = max(0, m.approvalCursor-1)
	case "down", "j":
		m.approvalCursor = min(max(0, len(m.pendingApprovals)-1), m.approvalCursor+1)
	case "enter":
		if _, ok := m.selectedApproval(); !ok {
			return m, nil
		}
		m.screen = screenConfirmApprove
	case "x":
		if _, ok := m.selectedApproval(); !ok {
			return m, nil
		}
		m.screen = screenConfirmReject
	case "r":
		return m, loadCmd(m.cfg, m.cwd)
	}
	return m, nil
}

// selectedApproval returns the pending request under the approval cursor, and
// false when the cursor is not on a real pending entry (empty list or an
// out-of-range index after a reload trimmed the requests).
func (m model) selectedApproval() (vault.EnrollmentRequest, bool) {
	if m.approvalCursor < 0 || m.approvalCursor >= len(m.pendingApprovals) {
		return vault.EnrollmentRequest{}, false
	}
	return m.pendingApprovals[m.approvalCursor], true
}

// confirmApproveKey approves the selected pending enrollment on y/Y and cancels
// on anything else. Approval re-encrypts every vault file for the new device
// and pushes, so it is slow: it runs through opCmd so the spinner shows while
// it works, and the success line tells the user the other computer can now
// unlock the vault.
func (m model) confirmApproveKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		request, ok := m.selectedApproval()
		if !ok {
			m.screen, m.info = screenDevices, "cancelled"
			return m, nil
		}
		cfg := *m.cfg
		requestID := request.ID
		m.screen, m.busy = screenDevices, true
		return m, opCmd(
			func() error { return app.ApproveDeviceEnrollment(cfg, requestID) },
			fmt.Sprintf("%s approved — that computer can now unlock the vault", request.Name),
		)
	}
	m.screen, m.info = screenDevices, "cancelled"
	return m, nil
}

// confirmRejectKey removes the request only after an explicit y/Y. Rejection
// grants no access and does not touch enrolled devices or encrypted profiles.
func (m model) confirmRejectKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		request, ok := m.selectedApproval()
		if !ok {
			m.screen, m.info = screenDevices, "cancelled"
			return m, nil
		}
		cfg := *m.cfg
		requestID := request.ID
		m.screen, m.busy = screenDevices, true
		return m, opCmd(
			func() error { return app.RejectDeviceEnrollment(cfg, requestID) },
			fmt.Sprintf("%s rejected — the request was removed", request.Name),
		)
	}
	m.screen, m.info = screenDevices, "cancelled"
	return m, nil
}
