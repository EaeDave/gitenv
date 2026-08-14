package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/eaedave/gitenv/internal/app"
)

// renderProjectList renders every vault project as a badged row: linked
// projects the user can open, and adoptable projects (found/missing/no-repo)
// the user can bring onto this machine with enter. The old name/profile/status
// row is gone because on a fresh machine most projects have no local link and
// no status to show — the badge plus a single hint carries the useful signal.
func (m model) renderLegacyProjectList() string {
	if len(m.projectStates) == 0 {
		return styles.muted.Render("No projects in the vault yet.\nPress a in a directory with .env to add one.")
	}
	anyLinked := false
	rows := make([]string, 0, len(m.projectStates)+2)
	for index, state := range m.projectStates {
		if state.Kind == app.ProjectLinked {
			anyLinked = true
		}
		rows = append(rows, m.renderProjectRow(index, state))
	}
	// The confusing state this feature fixes: the vault has projects but none is
	// linked here. Say so and point at the two ways forward.
	if !anyLinked {
		rows = append(rows, "", styles.muted.Render("None linked on this computer yet — press enter to adopt, or f to scan for clones."))
	}
	return strings.Join(rows, "\n")
}

// renderProjectRow draws one badged project row with a right-hand hint.
func (m model) renderProjectRow(index int, state app.ProjectState) string {
	name := state.Name
	if index == m.projectCursor {
		name = styles.selected.Render(name)
	} else {
		name = styles.value.Render(name)
	}
	return projectBadge(state.Kind) + " " + name + "  " + styles.muted.Render(projectHint(state))
}

// projectBadge maps a project state to its glyph, following the styles
// vocabulary: linked is settled (success), found/missing want action (warning),
// no-repo is inert (muted).
func projectBadge(kind app.ProjectStateKind) string {
	switch kind {
	case app.ProjectLinked:
		return styles.success.Render("●")
	case app.ProjectFound:
		return styles.warning.Render("◍")
	case app.ProjectMissing:
		return styles.warning.Render("◌")
	default: // app.ProjectNoRepo
		return styles.muted.Render("○")
	}
}

// projectHint is the right-hand column: the local path for anything already on
// disk, the repository identity for a missing clone, and a plain note when
// there is no repository to clone from.
func projectHint(state app.ProjectState) string {
	switch state.Kind {
	case app.ProjectLinked:
		return state.Path
	case app.ProjectFound:
		if len(state.Candidates) == 1 {
			return state.Candidates[0]
		}
		return fmt.Sprintf("%d local clones — press enter to choose", len(state.Candidates))
	case app.ProjectMissing:
		return state.Identity
	default: // app.ProjectNoRepo
		return "no repository recorded"
	}
}

// repositoryLabel is the safe repository identity to display for a project. The
// canonical identity is already credential-free; never build a display string
// from raw input.
func repositoryLabel(state app.ProjectState) string {
	if state.Identity != "" {
		return state.Identity
	}
	return "(no repository recorded)"
}

// renderAdoptForm renders a titled form panel with a context block above the
// fields so every adopt screen shows which project it is acting on.
func (m model) renderAdoptForm(title, context string, width int) string {
	panelWidth := min(width, 76)
	rows := make([]string, 0, len(m.fields)+2)
	if context != "" {
		rows = append(rows, context, "")
	}
	for index, f := range m.fields {
		rows = append(rows, renderField(f, index == m.fieldCursor, panelWidth))
	}
	panel := renderPanel(title, strings.Join(rows, "\n"), panelWidth, true)
	help := renderHelp("tab", "next field", "enter", "continue", "ctrl+u", "clear", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

// renderAdoptClone is the destination form for cloning a missing project.
func (m model) renderAdoptClone(width int) string {
	state, _ := m.projectStateByName(m.adoptName)
	context := labelValue("Project", m.adoptName) + "\n" +
		labelValue("Repository", repositoryLabel(state)) + "\n" +
		styles.muted.Render("No local clone found. Clone the repository, then link and apply its env file.")
	return m.renderAdoptForm("Clone and adopt "+m.adoptName, context, width)
}

// renderAdoptLink is the local-path form for linking a project already on disk.
func (m model) renderAdoptLink(width int) string {
	state, _ := m.projectStateByName(m.adoptName)
	context := labelValue("Project", m.adoptName) + "\n" +
		labelValue("Repository", repositoryLabel(state)) + "\n" +
		styles.muted.Render("Point gitenv at the local clone to link and apply its env file.")
	return m.renderAdoptForm("Link "+m.adoptName, context, width)
}

// renderAdoptProfile lists the only valid choices; users navigate instead of
// transcribing a profile name into a free-form field.
func (m model) renderAdoptProfile(width int) string {
	body := labelValue("Project", m.adoptName) + "\n" +
		styles.muted.Render("Choose which saved environment to apply after adoption:") + "\n\n" +
		renderMenu(m.adoptProfiles, m.menuCursor)
	panel := renderPanel("Choose a profile", body, min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("↑↓/jk", "select", "enter", "apply", "esc", "back"))
}

// renderAdoptCandidates is the picker shown when several clones match.
func (m model) renderAdoptCandidates(width int) string {
	context := labelValue("Project", m.adoptName) + "\n" +
		styles.value.Render("Several local clones match. Choose one to link:")
	body := context + "\n\n" + renderMenu(m.adoptCandidates, m.menuCursor)
	panel := renderPanel("Link a discovered clone", body, min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("↑↓", "select", "enter", "link", "esc", "cancel"))
}

// renderProjectOptions is the env-file / line-endings form for a project.
func (m model) renderProjectOptions(width int) string {
	context := labelValue("Project", m.adoptName) + "\n" +
		styles.muted.Render("Line endings: preserve · native · lf · crlf")
	return m.renderAdoptForm("Project options — "+m.adoptName, context, width)
}
