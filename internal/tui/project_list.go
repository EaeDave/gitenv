package tui

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
)

type projectFilter int

const (
	projectFilterAll projectFilter = iota
	projectFilterModified
	projectFilterMissing
)

type projectListItem struct {
	state   app.ProjectState
	current bool // current folder is not added yet
	focused bool // current folder is linked to this project
}

func (item projectListItem) FilterValue() string {
	return strings.Join([]string{item.state.Name, item.state.Path, item.state.Identity}, " ")
}

type projectListDelegate struct {
	hoveredIndex int
}

func (projectListDelegate) Height() int  { return 1 }
func (projectListDelegate) Spacing() int { return 0 }
func (projectListDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func (delegate projectListDelegate) Render(writer io.Writer, model list.Model, index int, raw list.Item) {
	item, ok := raw.(projectListItem)
	if !ok {
		return
	}
	selected := index == model.Index()
	hovered := index == delegate.hoveredIndex
	marker := "  "
	if selected {
		marker = styles.selected.Render("› ")
	} else if hovered {
		marker = styles.hovered.Render("• ")
	}

	nameStyle := styles.value
	if selected {
		nameStyle = styles.selected
	}
	if hovered {
		nameStyle = nameStyle.Underline(true)
	}

	badge := projectListBadge(item)
	rowWidth := max(8, model.Width()-6)
	fixed := marker + badge + " "
	summary := renderProjectListSummary(item)
	status := renderProjectListStatus(item)
	minimumNameWidth := 8
	if rowWidth-lipglossWidth(fixed)-lipglossWidth(summary)-2 < minimumNameWidth {
		// The profile is useful context, but status is the decision-making
		// signal. Drop profile context before ever truncating the status.
		summary = status
	}
	nameText := item.state.Name
	if item.focused {
		nameText = "⌂ " + nameText
	}
	nameWidth := rowWidth - lipglossWidth(fixed) - lipglossWidth(summary) - 2
	minimumUsefulName := min(14, lipglossWidth(nameText))
	if nameWidth < minimumUsefulName {
		// On genuinely narrow layouts the project name wins. The semantic dot
		// remains visible and the full status is available in Details.
		summary = ""
		nameWidth = rowWidth - lipglossWidth(fixed)
	}
	name := ansi.Truncate(nameStyle.Render(nameText), max(1, nameWidth), "…")
	row := fixed + name
	if summary != "" {
		row += strings.Repeat(" ", max(2, rowWidth-lipglossWidth(row)-lipglossWidth(summary))) + summary
	}
	row += strings.Repeat(" ", max(0, rowWidth-lipglossWidth(row)))
	if selected {
		row = styles.selectedRow.Render(row)
	} else if hovered {
		row = styles.hoveredRow.Render(row)
	}
	_, _ = io.WriteString(writer, row)
}

func renderProjectListSummary(item projectListItem) string {
	if item.current {
		return styles.warning.Render("not added")
	}
	state := item.state
	parts := make([]string, 0, 2)
	if state.ActiveProfile != "" {
		parts = append(parts, styles.muted.Render(state.ActiveProfile))
	}
	parts = append(parts, renderProjectListStatus(item))
	return strings.Join(parts, styles.muted.Render(" · "))
}

func renderProjectListStatus(item projectListItem) string {
	if item.current {
		return styles.warning.Render("not added")
	}
	state := item.state
	label := projectStateLabel(state)
	switch {
	case state.Kind != app.ProjectLinked:
		return styles.muted.Render(label)
	case state.Status == "clean" || state.Status == "synced":
		return styles.success.Render(label)
	case state.Status == "modified" || state.Status == "dirty" || state.Status == "missing" || state.Status == "unmanaged":
		return styles.warning.Render(label)
	case state.Status == "error":
		return styles.danger.Render(label)
	default:
		return styles.muted.Render(label)
	}
}

func projectListBadge(item projectListItem) string {
	if item.current {
		return styles.warning.Render("+")
	}
	state := item.state
	if state.Kind != app.ProjectLinked {
		if state.Kind == app.ProjectFound {
			return styles.warning.Render("◍")
		}
		return styles.muted.Render("○")
	}
	switch state.Status {
	case "clean", "synced":
		return styles.success.Render("●")
	case "modified", "dirty", "missing", "unmanaged":
		return styles.warning.Render("●")
	case "error":
		return styles.danger.Render("●")
	default:
		return styles.muted.Render("●")
	}
}

func projectStateLabel(state app.ProjectState) string {
	if state.Kind != app.ProjectLinked {
		switch state.Kind {
		case app.ProjectFound:
			return "clone found"
		case app.ProjectMissing:
			return "no local copy"
		default:
			return "no repository"
		}
	}
	switch state.Status {
	case "clean", "synced":
		return "up to date"
	case "modified", "dirty":
		return "modified"
	case "missing":
		return "env missing"
	case "unmanaged":
		return "not captured"
	case "error":
		return "error"
	default:
		return "checking"
	}
}

func newProjectList(items []list.Item, width, height int, isDark bool) *list.Model {
	projectList := list.New(items, projectListDelegate{hoveredIndex: -1}, max(24, width), max(5, height))
	projectList.SetShowTitle(false)
	projectList.SetShowHelp(false)
	projectList.SetShowStatusBar(true)
	projectList.SetShowPagination(true)
	projectList.SetStatusBarItemName("project", "projects")
	projectList.DisableQuitKeybindings()
	projectList.Styles = list.DefaultStyles(isDark)
	return &projectList
}

func (m model) projectListItems() []list.Item {
	items := make([]list.Item, 0, len(m.projectStates)+1)
	if m.current.HasEnv && m.current.LinkedName == "" && m.current.Path != "" {
		item := projectListItem{
			state: app.ProjectState{
				Name: m.current.Name,
				Kind: app.ProjectNoRepo,
				Path: m.current.Path,
			},
			current: true,
		}
		if m.projectFilterMatches(item) {
			items = append(items, item)
		}
	}
	for _, state := range m.projectStates {
		item := projectListItem{state: state, focused: state.Name == m.current.LinkedName}
		if m.projectFilterMatches(item) {
			items = append(items, item)
		}
	}
	return items
}

func (m model) projectFilterMatches(item projectListItem) bool {
	if item.current {
		return m.projectFilter == projectFilterAll
	}
	switch m.projectFilter {
	case projectFilterModified:
		return item.state.Kind == app.ProjectLinked && (item.state.Status == "modified" || item.state.Status == "dirty")
	case projectFilterMissing:
		return item.state.Kind == app.ProjectMissing
	default:
		return true
	}
}

func (m *model) setProjectFilter(filter projectFilter) {
	if m.projectFilter == filter {
		return
	}
	m.projectFilter = filter
	m.refreshProjectList()
}

func (m *model) cycleProjectFilter(direction int) {
	count := int(projectFilterMissing) + 1
	next := (int(m.projectFilter) + direction + count) % count
	m.setProjectFilter(projectFilter(next))
}

func (m *model) refreshProjectList() {
	selectedName, selectedCurrent := "", false
	if selected, ok := m.selectedProjectListItem(); ok {
		selectedName, selectedCurrent = selected.state.Name, selected.current
	}
	items := m.projectListItems()
	if m.projectList == nil {
		m.projectList = newProjectList(items, m.projectListWidth(), m.projectListHeight(), m.isDark)
	} else {
		m.projectList.SetItems(items)
		m.resizeProjectList()
	}
	for index, raw := range items {
		item := raw.(projectListItem)
		if item.state.Name == selectedName && item.current == selectedCurrent {
			m.projectList.Select(index)
			break
		}
	}
	m.syncProjectCursor()
}

func (m *model) resizeProjectList() {
	if m.projectList == nil {
		return
	}
	m.projectList.SetSize(m.projectListWidth(), m.projectListHeight())
}

func projectListPanelWidth(width int) int {
	if width >= compactViewWidth {
		return max(32, width*2/5)
	}
	return width
}

func (m model) projectListWidth() int {
	return projectListPanelWidth(availableWidth(m.width)) - 4
}

func (m model) projectListHeight() int {
	if m.height <= 0 {
		return 10
	}
	if availableWidth(m.width) >= dashboardViewWidth && m.height >= 28 {
		// Sync and overview live in the right column on a roomy terminal, so the
		// project list can use the full workspace height.
		return max(5, m.height-11)
	}
	return max(5, m.height-18)
}

func (m model) selectedProjectListItem() (projectListItem, bool) {
	if m.projectList == nil || m.projectList.SelectedItem() == nil {
		return projectListItem{}, false
	}
	item, ok := m.projectList.SelectedItem().(projectListItem)
	return item, ok
}

func (m *model) syncProjectCursor() {
	item, ok := m.selectedProjectListItem()
	if !ok || item.current {
		return
	}
	for index, state := range m.projectStates {
		if state.Name == item.state.Name {
			m.projectCursor = index
			return
		}
	}
}

func (m model) updateProjectList(key tea.KeyPressMsg) (model, tea.Cmd, bool) {
	if m.projectList == nil {
		return m, nil, false
	}
	filtering := m.projectList.SettingFilter()
	if filtering || key.String() == "/" || (key.String() == "esc" && m.projectList.IsFiltered()) {
		updated, cmd := m.projectList.Update(key)
		*m.projectList = updated
		m.syncProjectCursor()
		return m, cmd, true
	}
	switch key.String() {
	case "up", "down", "j", "k", "pgup", "pgdown", "home", "end":
		updated, cmd := m.projectList.Update(key)
		*m.projectList = updated
		m.syncProjectCursor()
		return m, cmd, true
	}
	return m, nil, false
}

func lipglossWidth(value string) int {
	return ansi.StringWidth(value)
}

func (m model) renderProjectList() string {
	return m.projectListView(m.projectListWidth(), m.projectListHeight())
}

func (m model) projectListView(width, height int) string {
	if m.projectList == nil {
		return m.renderLegacyProjectList()
	}
	copy := *m.projectList
	hoveredIndex := -1
	if m.hoveredMouseTarget.kind == mouseTargetProjectRow {
		hoveredIndex = m.hoveredMouseTarget.index
	}
	copy.SetDelegate(projectListDelegate{hoveredIndex: hoveredIndex})
	copy.SetSize(max(20, width), max(5, height))
	return copy.View()
}
