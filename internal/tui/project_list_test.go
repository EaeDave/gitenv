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
	if !strings.Contains(listView, "prod · mo") {
		t.Fatalf("compact row lost profile/status context:\n%s", listView)
	}
}
