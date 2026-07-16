package tui

import "github.com/charmbracelet/lipgloss"

const (
	defaultViewWidth = 80
	compactViewWidth = 72
)

var colors = struct {
	primary, accent, text, muted, success, warning, danger lipgloss.AdaptiveColor
}{
	primary: lipgloss.AdaptiveColor{Light: "#5B35D5", Dark: "#9B87F5"},
	accent:  lipgloss.AdaptiveColor{Light: "#006D77", Dark: "#5EEAD4"},
	text:    lipgloss.AdaptiveColor{Light: "#20202A", Dark: "#F4F1FF"},
	muted:   lipgloss.AdaptiveColor{Light: "#666273", Dark: "#918BA6"},
	success: lipgloss.AdaptiveColor{Light: "#167647", Dark: "#52D68A"},
	warning: lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#F5C451"},
	danger:  lipgloss.AdaptiveColor{Light: "#C5283D", Dark: "#FF6B7D"},
}

var styles = struct {
	brand, subtitle, title, label, value, muted, selected lipgloss.Style
	panel, activePanel, key, success, warning, danger     lipgloss.Style
}{
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
