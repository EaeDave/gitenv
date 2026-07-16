package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/eaedave/gitenv/internal/envdiff"
)

func (m model) renderCapturePreview(width int) string {
	rows := []string{
		labelValue("Project", m.pendingProject),
		labelValue("Profile", m.pendingProfile),
		"",
	}
	if m.captureDiff.Empty() {
		rows = append(rows, styles.muted.Render("No structural changes detected."))
	} else {
		for _, change := range m.captureDiff.Changes {
			rows = append(rows, renderCaptureChange(change))
		}
		if m.captureDiff.CommentChanges > 0 {
			rows = append(rows, styles.muted.Render(fmt.Sprintf("• %d comment change(s)", m.captureDiff.CommentChanges)))
		}
		if m.captureDiff.UnknownChanges > 0 {
			rows = append(rows, styles.warning.Render(fmt.Sprintf("! %d unrecognized line change(s)", m.captureDiff.UnknownChanges)))
		}
	}
	rows = append(rows, "", styles.muted.Render("Values are hidden. Capture preserves the file byte for byte."))
	panel := renderPanel("Capture local .env changes?", strings.Join(rows, "\n"), min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("y", "capture", "n/esc", "cancel"))
}

func renderCaptureChange(change envdiff.Change) string {
	switch change.Kind {
	case envdiff.Added:
		return styles.success.Render("+ " + change.Key)
	case envdiff.Removed:
		return styles.danger.Render("- " + change.Key)
	case envdiff.Enabled:
		return styles.success.Render("● " + change.Key + " enabled")
	case envdiff.Disabled:
		return styles.warning.Render("○ " + change.Key + " disabled")
	default:
		return styles.warning.Render("~ " + change.Key)
	}
}
