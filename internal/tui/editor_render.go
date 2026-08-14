package tui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m model) editorViewportHeight() int {
	lineCount := max(1, m.editor.LineCount())
	if m.height <= 0 {
		return min(16, max(6, lineCount))
	}
	available := max(4, m.height-18)
	return min(available, max(6, lineCount))
}

func (m model) editorGutterWidth() int {
	digits := len(fmt.Sprintf("%d", max(1, m.editor.LineCount())))
	return digits + 3 // line number, one space, separator, one space
}

func (m model) editorContentWidth() int {
	panelContentWidth := max(20, availableWidth(m.width)-4)
	return max(8, panelContentWidth-m.editorGutterWidth())
}

func (m model) renderEditorViewport() string {
	lines := strings.Split(m.editor.Value(), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	height := m.editorViewportHeight()
	top := min(max(0, m.editorTopLine), max(0, len(lines)-1))
	bottom := min(len(lines), top+height)
	digits := len(fmt.Sprintf("%d", len(lines)))
	contentWidth := m.editorContentWidth()
	rows := make([]string, 0, height)
	for lineIndex := top; lineIndex < bottom; lineIndex++ {
		active := lineIndex == m.editor.Line()
		hovered := m.hoveredMouseTarget.kind == mouseTargetEditorRow && m.hoveredMouseTarget.index == lineIndex
		lineNumberStyle := styles.muted
		if active {
			lineNumberStyle = styles.selected
		}
		gutter := lineNumberStyle.Render(fmt.Sprintf("%*d", digits, lineIndex+1)) + styles.muted.Render(" │ ")
		content := renderEnvEditorLine(
			lines[lineIndex],
			m.editorHorizontalOffset,
			contentWidth,
			active,
			m.editor.Column(),
		)
		row := gutter + content
		row += strings.Repeat(" ", max(0, digits+3+contentWidth-lipgloss.Width(row)))
		if active {
			row = styles.selectedRow.Render(row)
		} else if hovered {
			row = styles.hoveredRow.Render(row)
		}
		rows = append(rows, row)
	}
	for len(rows) < height {
		gutter := styles.muted.Render(strings.Repeat(" ", digits) + " │ ")
		rows = append(rows, gutter+styles.muted.Render("~"))
	}
	return strings.Join(rows, "\n")
}

func renderEnvEditorLine(line string, horizontalOffset, width int, active bool, cursorColumn int) string {
	runes := []rune(line)
	horizontalOffset = min(max(0, horizontalOffset), len(runes))
	visibleEnd := horizontalOffset
	visibleWidth := 0
	for visibleEnd < len(runes) {
		runeWidth := max(1, ansi.StringWidth(string(runes[visibleEnd])))
		if visibleWidth+runeWidth > width {
			break
		}
		visibleWidth += runeWidth
		visibleEnd++
	}

	var out strings.Builder
	segmentStart := horizontalOffset
	segmentKind := envEditorTokenKind(line, horizontalOffset)
	flush := func(end int) {
		if end <= segmentStart {
			return
		}
		out.WriteString(envEditorTokenStyle(segmentKind).Render(string(runes[segmentStart:end])))
	}
	for index := horizontalOffset; index < visibleEnd; index++ {
		kind := envEditorTokenKind(line, index)
		if kind != segmentKind {
			flush(index)
			segmentStart = index
			segmentKind = kind
		}
		if active && index == cursorColumn {
			flush(index)
			out.WriteString(styles.editorCursor.Render(string(runes[index])))
			segmentStart = index + 1
			if segmentStart < visibleEnd {
				segmentKind = envEditorTokenKind(line, segmentStart)
			}
		}
	}
	flush(visibleEnd)
	if active && cursorColumn == len(runes) && visibleWidth < width {
		out.WriteString(styles.editorCursor.Render(" "))
	}
	if visibleEnd < len(runes) && width > 0 {
		return ansi.Truncate(out.String(), max(1, width-1), "") + styles.muted.Render("…")
	}
	return out.String()
}

type envEditorToken int

const (
	envEditorValue envEditorToken = iota
	envEditorKey
	envEditorSeparator
	envEditorComment
)

func envEditorTokenStyle(token envEditorToken) lipgloss.Style {
	switch token {
	case envEditorKey:
		return styles.key
	case envEditorSeparator, envEditorComment:
		return styles.muted
	default:
		return styles.value
	}
}

func envEditorTokenKind(line string, runeIndex int) envEditorToken {
	runes := []rune(line)
	firstContent := 0
	for firstContent < len(runes) && unicode.IsSpace(runes[firstContent]) {
		firstContent++
	}
	if firstContent < len(runes) && runes[firstContent] == '#' {
		return envEditorComment
	}
	equals := -1
	for index, character := range runes {
		if character == '=' {
			equals = index
			break
		}
	}
	if equals < 0 {
		return envEditorValue
	}
	switch {
	case runeIndex < equals && !unicode.IsSpace(runes[runeIndex]):
		return envEditorKey
	case runeIndex == equals:
		return envEditorSeparator
	default:
		return envEditorValue
	}
}
