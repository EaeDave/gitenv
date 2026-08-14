package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/eaedave/gitenv/internal/envdiff"
)

func (m model) renderEditor(width int) string {
	title := styles.brand.Render("Edit .env") + "  " + styles.value.Render(m.editorProject) + "  " +
		styles.warning.Render("● values visible")
	position := fmt.Sprintf("Line %d/%d · Col %d", m.editor.Line()+1, max(1, m.editor.LineCount()), m.editor.Column()+1)
	panelTitle := "Local .env  ·  " + position
	editorPanel := renderPanel(panelTitle, m.renderEditorViewport(), width, true)
	summary := m.renderEditorDiff()
	help := lipgloss.JoinVertical(lipgloss.Left,
		renderHelp("ctrl+s", "save", "esc", "cancel", "click", "move cursor", "wheel", "scroll", "enter", "new line"),
		renderHelp("shift+drag", "select terminal text"),
	)
	sections := []string{
		title,
		"",
		editorPanel,
		"",
		summary,
		"",
		m.renderMouseButtons(editorMouseButtons()),
		"",
		help,
	}
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
