package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

const (
	defaultViewWidth = 80
	compactViewWidth = 72
)

type palette struct {
	primary color.Color
	accent  color.Color
	text    color.Color
	muted   color.Color
	success color.Color
	warning color.Color
	danger  color.Color
}

type themeStyles struct {
	brand       lipgloss.Style
	subtitle    lipgloss.Style
	title       lipgloss.Style
	label       lipgloss.Style
	value       lipgloss.Style
	muted       lipgloss.Style
	selected    lipgloss.Style
	panel       lipgloss.Style
	activePanel lipgloss.Style
	key         lipgloss.Style
	success     lipgloss.Style
	warning     lipgloss.Style
	danger      lipgloss.Style
}

var (
	colors = colorsForBackground(true)
	styles = stylesForPalette(colors)
)

func applyTheme(isDark bool) {
	colors = colorsForBackground(isDark)
	styles = stylesForPalette(colors)
}

func colorsForBackground(isDark bool) palette {
	pick := func(light, dark string) color.Color {
		if isDark {
			return lipgloss.Color(dark)
		}
		return lipgloss.Color(light)
	}
	return palette{
		primary: pick("#5B35D5", "#9B87F5"),
		accent:  pick("#006D77", "#5EEAD4"),
		text:    pick("#20202A", "#F4F1FF"),
		muted:   pick("#666273", "#918BA6"),
		success: pick("#167647", "#52D68A"),
		warning: pick("#9A6700", "#F5C451"),
		danger:  pick("#C5283D", "#FF6B7D"),
	}
}

func stylesForPalette(colors palette) themeStyles {
	return themeStyles{
		brand:       lipgloss.NewStyle().Bold(true).Foreground(colors.primary),
		subtitle:    lipgloss.NewStyle().Foreground(colors.muted),
		title:       lipgloss.NewStyle().Bold(true).Foreground(colors.text),
		label:       lipgloss.NewStyle().Foreground(colors.muted),
		value:       lipgloss.NewStyle().Foreground(colors.text),
		muted:       lipgloss.NewStyle().Foreground(colors.muted),
		selected:    lipgloss.NewStyle().Bold(true).Foreground(colors.primary),
		panel:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colors.muted).Padding(0, 1),
		activePanel: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colors.primary).Padding(0, 1),
		key:         lipgloss.NewStyle().Bold(true).Foreground(colors.accent),
		success:     lipgloss.NewStyle().Foreground(colors.success),
		warning:     lipgloss.NewStyle().Foreground(colors.warning),
		danger:      lipgloss.NewStyle().Foreground(colors.danger),
	}
}

func availableWidth(width int) int {
	if width <= 0 {
		return defaultViewWidth
	}
	if width < 40 {
		return 40
	}
	return width
}

func renderPanel(title, body string, width int, active bool) string {
	panelStyle := styles.panel
	if active {
		panelStyle = styles.activePanel
	}
	contentWidth := max(20, width-4)
	heading := styles.title.Render(title)
	return panelStyle.Width(contentWidth).Render(heading + "\n\n" + body)
}

func renderHelp(bindings ...string) string {
	parts := make([]string, 0, len(bindings))
	for index := 0; index+1 < len(bindings); index += 2 {
		parts = append(parts, styles.key.Render(bindings[index])+" "+styles.muted.Render(bindings[index+1]))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, joinWithSpace(parts...)...)
}

func joinWithSpace(parts ...string) []string {
	if len(parts) < 2 {
		return parts
	}
	result := make([]string, 0, len(parts)*2-1)
	for index, part := range parts {
		if index > 0 {
			result = append(result, "   ")
		}
		result = append(result, part)
	}
	return result
}
