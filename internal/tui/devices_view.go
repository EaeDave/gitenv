package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/eaedave/gitenv/internal/app"
)

// renderDevices draws the device roster as one panel with two sections:
// requests waiting for approval, and the devices the vault already trusts. A
// request id is deliberately never rendered — the whole point of this screen is
// that ids stop being something users copy between machines.
func (m model) renderDevices(width int) string {
	rows := []string{styles.label.Render("Waiting for approval")}
	if len(m.pendingApprovals) == 0 {
		rows = append(rows,
			styles.muted.Render("Nothing is waiting for approval."),
			styles.muted.Render("A new computer asks to join from its own unlock screen;"),
			styles.muted.Render("the request then appears here for a trusted device to approve."),
		)
	} else {
		for index, request := range m.pendingApprovals {
			waited := styles.muted.Render("requested " + humanizeSince(request.CreatedAt))
			if index == m.approvalCursor {
				rows = append(rows, styles.selected.Render("› "+request.Name)+"  "+waited)
			} else {
				rows = append(rows, "  "+styles.value.Render(request.Name)+"  "+waited)
			}
		}
	}

	rows = append(rows, "", styles.label.Render("Enrolled"))
	enrolled := app.EnrolledDevices(m.manifest)
	if len(enrolled) == 0 {
		rows = append(rows, styles.muted.Render("No devices are enrolled yet."))
	} else {
		for _, device := range enrolled {
			rows = append(rows, "  "+styles.value.Render(device.Name)+"  "+styles.muted.Render("trusted — can decrypt every environment"))
		}
	}

	panel := renderPanel("Devices", strings.Join(rows, "\n"), min(width, 76), true)
	help := renderHelp("↑↓", "select", "enter", "approve", "r", "reload", "?", "help", "esc", "back")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

// renderConfirmApprove asks the user to confirm approving a device. Approval is
// a security decision, so the prompt names the device and says plainly what it
// grants: permission to decrypt every environment in the vault, and that it
// cannot be undone from here.
func (m model) renderConfirmApprove(width int) string {
	name := "this device"
	if request, ok := m.selectedApproval(); ok {
		name = request.Name
	}
	message := fmt.Sprintf(
		"Approve %q?\n%s will be able to decrypt every environment in this vault.\nThis cannot be undone from here. [y/N]",
		name, name,
	)
	return m.renderConfirmation("Approve device?", message, width)
}

// humanizeSince renders how long ago t was in plain terms, so the roster reads
// "2 hours ago" instead of a machine timestamp. A zero or future time reads as
// "just now" rather than showing a negative or nonsensical duration.
func humanizeSince(t time.Time) string {
	if t.IsZero() {
		return "just now"
	}
	elapsed := time.Since(t)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		minutes := int(elapsed / time.Minute)
		return fmt.Sprintf("%d %s ago", minutes, pluralize(minutes, "minute", "minutes"))
	case elapsed < 24*time.Hour:
		hours := int(elapsed / time.Hour)
		return fmt.Sprintf("%d %s ago", hours, pluralize(hours, "hour", "hours"))
	default:
		days := int(elapsed / (24 * time.Hour))
		return fmt.Sprintf("%d %s ago", days, pluralize(days, "day", "days"))
	}
}
