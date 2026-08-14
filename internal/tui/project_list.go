package tui

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
)

type projectListItem struct {
	state   app.ProjectState
	current bool
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

	name := item.state.Name
	if selected && hovered {
		name = styles.selected.Underline(true).Render(name)
	} else if selected {
		name = styles.selected.Render(name)
	} else if hovered {
		name = styles.hovered.Render(name)
	} else {
		name = styles.value.Render(name)
	}

	badge := projectBadge(item.state.Kind)
	if item.current {
		badge = styles.warning.Render("+")
	}
	summary := projectListSummary(item)
	prefix := marker + badge + " " + name
	rowWidth := max(8, model.Width()-6)
	available := max(0, rowWidth-lipglossWidth(prefix)-2)
	if summary != "" && available >= 6 {
		summary = ansi.Truncate(summary, available, "…")
		prefix += "  " + styles.muted.Render(summary)
	}
	_, _ = io.WriteString(writer, ansi.Truncate(prefix, rowWidth, "…"))
}

func projectListSummary(item projectListItem) string {
	if item.current {
		return "not added"
	}
	state := item.state
	parts := make([]string, 0, 2)
	if state.ActiveProfile != "" {
		parts = append(parts, state.ActiveProfile)
	}
	parts = append(parts, projectStateLabel(state))
	return strings.Join(parts, " · ")
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
		items = append(items, projectListItem{
			state: app.ProjectState{
				Name: m.current.Name,
				Kind: app.ProjectNoRepo,
				Path: m.current.Path,
			},
			current: true,
		})
	}
	for _, state := range m.projectStates {
		items = append(items, projectListItem{state: state})
	}
	return items
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
