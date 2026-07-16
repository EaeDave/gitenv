package tui

import (
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
	if !m.editorBaseAvailable {
		return styles.muted.Render("new .env — no captured profile to compare against")
	}
	title := styles.label.Render("Diff vs " + m.editorBaseProfile + " (captured)")
	changes := renderLiteralLineChanges(envdiff.CompareLines(m.editorBase, m.editorBytes()))
	if len(changes) == 0 {
		return title + "\n" + styles.success.Render("  local .env matches the captured profile")
	}
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{title}, changes...)...)
}
