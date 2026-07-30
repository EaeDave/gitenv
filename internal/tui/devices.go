package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

// devicesKey drives the device roster. The cursor only ever lands on a pending
// approval, because approving is the single action available here; enrolled
// devices are listed for reassurance, not to act on.
func (m model) devicesKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		m.approvalCursor = max(0, m.approvalCursor-1)
	case "down", "j":
		m.approvalCursor = min(max(0, len(m.pendingApprovals)-1), m.approvalCursor+1)
	case "enter":
		if _, ok := m.selectedApproval(); !ok {
			// The cursor is on an enrolled device (or the list is empty).
			// There is nothing to approve, so opening the confirmation would
			// be a dead screen.
			return m, nil
		}
		m.screen = screenConfirmApprove
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
func (m model) confirmApproveKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
