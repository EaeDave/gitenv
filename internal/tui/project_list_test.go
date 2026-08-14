package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

func TestCurrentFolderAppearsAsAddableProject(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{
		cfg:     &cfg,
		screen:  screenProjects,
		width:   100,
		height:  28,
		isDark:  true,
		current: app.CurrentProject{Name: "promex-tt-mill", Path: "/workspace/promex-tt-mill", HasEnv: true},
	}
	m.refreshProjectList()

	view := ansi.Strip(m.View().Content)
	for _, expected := range []string{"promex-tt-mill", "Current folder is not in gitenv yet", "not added", "enter/a  add and capture this project", "a add current"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("current project call to action missing %q:\n%s", expected, view)
		}
	}

	next, cmd := m.projectsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := next.(model)
	if cmd != nil || got.screen != screenAddProject {
		t.Fatalf("enter did not open the add-current form: screen=%v cmd=%v", got.screen, cmd)
	}
}

func TestProjectListScrollsWithinTerminalHeight(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	states := make([]app.ProjectState, 12)
	for index := range states {
		name := fmt.Sprintf("project-%02d", index)
		states[index] = app.ProjectState{Name: name, Kind: app.ProjectLinked, Path: "/workspace/" + name, ActiveProfile: "dev", Status: "clean"}
	}
	m := model{cfg: &cfg, screen: screenProjects, width: 80, height: 22, isDark: true, recoveryExported: true, projectStates: states, projects: projectNames(states)}
	m.refreshProjectList()

	for range 10 {
		next, _ := m.projectsKey(tea.KeyPressMsg{Code: tea.KeyDown})
		m = next.(model)
	}
	selected, ok := m.selectedProjectState()
	if !ok || selected.Name != "project-10" {
		t.Fatalf("scrolling lost selection: %#v", selected)
	}
	visible := ansi.Strip(m.renderProjectList())
	if !strings.Contains(visible, "project-10") {
		t.Fatalf("selected project scrolled out of view:\n%s", visible)
	}
	if strings.Contains(visible, "project-00") {
		t.Fatalf("list rendered every project instead of using its viewport:\n%s", visible)
	}
	if renderedHeight := lipgloss.Height(m.View().Content); renderedHeight > m.height {
		t.Fatalf("responsive view is %d lines in a %d-line terminal:\n%s", renderedHeight, m.height, ansi.Strip(m.View().Content))
	}
}

func TestProjectListColorsStatusByMeaning(t *testing.T) {
	cases := []struct {
		name   string
		state  app.ProjectState
		styled string
		badge  string
	}{
		{"clean", app.ProjectState{Kind: app.ProjectLinked, Status: "clean"}, styles.success.Render("up to date"), styles.success.Render("●")},
		{"modified", app.ProjectState{Kind: app.ProjectLinked, Status: "modified"}, styles.warning.Render("modified"), styles.warning.Render("●")},
		{"missing clone", app.ProjectState{Kind: app.ProjectMissing}, styles.muted.Render("no local copy"), styles.muted.Render("○")},
		{"error", app.ProjectState{Kind: app.ProjectLinked, Status: "error"}, styles.danger.Render("error"), styles.danger.Render("●")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := projectListItem{state: tc.state}
			if summary := renderProjectListSummary(item); !strings.Contains(summary, tc.styled) {
				t.Fatalf("summary %q does not contain semantic style %q", summary, tc.styled)
			}
			if badge := projectListBadge(item); badge != tc.badge {
				t.Fatalf("badge %q, want %q", badge, tc.badge)
			}
		})
	}
}

func TestWideProjectDashboardUsesRightColumnForOverviewAndSync(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	states := []app.ProjectState{
		{Name: "clean", Kind: app.ProjectLinked, Status: "clean"},
		{Name: "changed", Kind: app.ProjectLinked, Status: "modified"},
		{Name: "remote-only", Kind: app.ProjectMissing},
	}
	m := model{cfg: &cfg, screen: screenProjects, width: 120, height: 40, recoveryExported: true, projectStates: states, projects: projectNames(states)}
	m.refreshProjectList()
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	positions := map[string][2]int{}
	for y, line := range lines {
		for _, title := range []string{"Details", "Overview", "Sync"} {
			if x := strings.Index(line, title); x >= 0 {
				positions[title] = [2]int{x, y}
			}
		}
	}
	if positions["Overview"][0] <= 0 || positions["Details"][1] >= positions["Overview"][1] || positions["Overview"][1] >= positions["Sync"][1] {
		t.Fatalf("right dashboard panels are not stacked: %#v\n%s", positions, view)
	}
	for _, expected := range []string{"1 modified", "1 no local copy", "1 up to date", "[ All ]", "[ Modified ]", "[ Missing ]"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("overview missing %q:\n%s", expected, view)
		}
	}
	if renderedHeight := lipgloss.Height(m.View().Content); renderedHeight > m.height {
		t.Fatalf("dashboard is %d lines in a %d-line terminal", renderedHeight, m.height)
	}
}

func TestProjectFiltersCycleWithoutLosingKeyboardNavigation(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	states := []app.ProjectState{
		{Name: "clean", Kind: app.ProjectLinked, Status: "clean"},
		{Name: "changed", Kind: app.ProjectLinked, Status: "modified"},
		{Name: "remote-only", Kind: app.ProjectMissing},
	}
	m := model{cfg: &cfg, screen: screenProjects, width: 120, height: 32, projectStates: states, projects: projectNames(states)}
	m.refreshProjectList()

	next, _ := m.projectsKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(model)
	if m.projectFilter != projectFilterModified || len(m.projectList.VisibleItems()) != 1 {
		t.Fatalf("tab did not select modified filter: filter=%v items=%d", m.projectFilter, len(m.projectList.VisibleItems()))
	}
	if title := projectListTitle(m.projectFilter, 1, 3); title != "Projects · 1/3 · modified" {
		t.Fatalf("filtered title = %q", title)
	}

	next, _ = m.projectsKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(model)
	if item := m.projectList.SelectedItem().(projectListItem); m.projectFilter != projectFilterMissing || item.state.Name != "remote-only" {
		t.Fatalf("second tab did not select missing projects: filter=%v item=%q", m.projectFilter, item.state.Name)
	}
	next, _ = m.projectsKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = next.(model)
	if m.projectFilter != projectFilterModified {
		t.Fatalf("shift+tab filter = %v, want modified", m.projectFilter)
	}
}

func TestProjectListSupportsFuzzySearchAndHidesLongPaths(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	longPath := "/home/example/Projects/company/platform/services/promex-tt-mill"
	states := []app.ProjectState{
		{Name: "api", Kind: app.ProjectLinked, Path: "/workspace/api", Status: "clean"},
		{Name: "promex-tt-mill", Kind: app.ProjectLinked, Path: longPath, ActiveProfile: "prod", Status: "modified"},
	}
	m := model{cfg: &cfg, screen: screenProjects, width: 120, height: 24, isDark: true, projectStates: states, projects: projectNames(states)}
	m.refreshProjectList()

	m.projectList.SetFilterText("ptm")
	if len(m.projectList.VisibleItems()) != 1 {
		t.Fatalf("fuzzy search returned %d items", len(m.projectList.VisibleItems()))
	}
	item := m.projectList.VisibleItems()[0].(projectListItem)
	if item.state.Name != "promex-tt-mill" {
		t.Fatalf("fuzzy search selected %q", item.state.Name)
	}
	listView := ansi.Strip(m.renderProjectList())
	if strings.Contains(listView, longPath) {
		t.Fatalf("full path leaked into compact project row:\n%s", listView)
	}
	if !strings.Contains(listView, "prod · modified") {
		t.Fatalf("wide row truncated decision-making status:\n%s", listView)
	}
	if !strings.Contains(m.renderProjectList(), "\x1b[48;2;") {
		t.Fatalf("selected row has no background emphasis:\n%s", m.renderProjectList())
	}
}
