package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/eaedave/gitenv/internal/envdiff"
)

func (m model) renderEditor(width int) string {
	title := styles.brand.Render("Edit .env") + "  " + styles.value.Render(m.editorProject)
	summary := m.renderEditorDiff()
	help := renderHelp("ctrl+s", "save", "esc", "cancel", "↑↓/←→", "move", "enter", "new line")
	sections := []string{title, "", m.editor.View(), "", summary, "", help}
	_ = width
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m model) renderEditorDiff() string {
	diff := envdiff.Compare(m.editorRaw, m.editorBytes())
	if diff.Empty() {
		return styles.muted.Render("no changes yet")
	}
	lines := []string{styles.label.Render("Pending changes")}
	for _, change := range diff.Changes {
		lines = append(lines, "  "+renderCaptureChange(change))
	}
	if diff.CommentChanges > 0 {
		lines = append(lines, styles.muted.Render(fmt.Sprintf("  • %d comment change(s)", diff.CommentChanges)))
	}
	if diff.UnknownChanges > 0 {
		lines = append(lines, styles.warning.Render(fmt.Sprintf("  ! %d unrecognized line change(s)", diff.UnknownChanges)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
