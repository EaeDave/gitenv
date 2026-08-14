package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

func mouseProjectsModel(count int) model {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	states := make([]app.ProjectState, count)
	manifestProjects := make(map[string]vault.Project, count)
	for index := range count {
		name := "project-" + string(rune('a'+index))
		path := "/workspace/" + name
		cfg.Projects[name] = vault.LocalProject{Path: path, ActiveProfile: "dev"}
		states[index] = app.ProjectState{Name: name, Kind: app.ProjectLinked, Path: path, ActiveProfile: "dev", Status: "clean"}
		manifestProjects[name] = vault.Project{Profiles: map[string]vault.Profile{"dev": {}}}
	}
	m := model{
		cfg:              &cfg,
		manifest:         vault.Manifest{Projects: manifestProjects},
		screen:           screenProjects,
		width:            100,
		height:           28,
		isDark:           true,
		recoveryExported: true,
		projectStates:    states,
		projects:         projectNames(states),
		statuses:         map[string]string{},
	}
	m.refreshProjectList()
	return m
}

func regionForTarget(t *testing.T, regions []mouseRegion, target mouseTarget) mouseRegion {
	t.Helper()
	for _, region := range regions {
		if region.target == target {
			return region
		}
	}
	t.Fatalf("mouse target not rendered: %#v", target)
	return mouseRegion{}
}

func dispatchViewMouse(t *testing.T, m model, msg tea.MouseMsg) model {
	t.Helper()
	cmd := m.View().OnMouse(msg)
	if cmd == nil {
		t.Fatalf("mouse event %T produced no command", msg)
	}
	next, _ := m.Update(cmd())
	return next.(model)
}

func clickViewTarget(t *testing.T, m model, region mouseRegion) model {
	t.Helper()
	m = dispatchViewMouse(t, m, tea.MouseClickMsg{X: region.bounds.x, Y: region.bounds.y, Button: tea.MouseLeft})
	return dispatchViewMouse(t, m, tea.MouseReleaseMsg{X: region.bounds.x, Y: region.bounds.y, Button: tea.MouseLeft})
}

func TestViewEnablesMouseAndProjectHoverFeedback(t *testing.T) {
	m := mouseProjectsModel(3)
	view := m.View()
	if view.MouseMode != tea.MouseModeAllMotion || !view.ReportFocus || view.OnMouse == nil {
		t.Fatalf("mouse support is not declared in the v2 view: mode=%v focus=%v handler=%v", view.MouseMode, view.ReportFocus, view.OnMouse != nil)
	}
	region := regionForTarget(t, m.mouseRegions(view.Content), mouseTarget{kind: mouseTargetProjectRow, index: 1})
	motion := tea.MouseMotionMsg{X: region.bounds.x, Y: region.bounds.y}
	m = dispatchViewMouse(t, m, motion)
	if m.hoveredMouseTarget != region.target {
		t.Fatalf("hover target = %#v, want %#v", m.hoveredMouseTarget, region.target)
	}
	if rendered := ansi.Strip(m.renderProjectList()); !strings.Contains(rendered, "• ● project-b") {
		t.Fatalf("hovered row has no visual feedback:\n%s", rendered)
	}
	if cmd := m.View().OnMouse(motion); cmd != nil {
		t.Fatal("motion inside the same target should not trigger another render")
	}
}

func TestProjectMouseClickSelectsAndDoubleClickOpens(t *testing.T) {
	m := mouseProjectsModel(3)
	region := regionForTarget(t, m.mouseRegions(m.View().Content), mouseTarget{kind: mouseTargetProjectRow, index: 1})
	click := tea.MouseClickMsg{X: region.bounds.x, Y: region.bounds.y, Button: tea.MouseLeft}

	m = dispatchViewMouse(t, m, click)
	selected, ok := m.selectedProjectState()
	if !ok || selected.Name != "project-b" || m.screen != screenProjects {
		t.Fatalf("single click should only select: selected=%#v screen=%v", selected, m.screen)
	}

	m = dispatchViewMouse(t, m, click)
	if m.screen != screenProfiles || m.selectedProject != "project-b" {
		t.Fatalf("double click did not open selected project: screen=%v selected=%q", m.screen, m.selectedProject)
	}
}

func TestProjectMouseWheelScrollsAndButtonsAct(t *testing.T) {
	m := mouseProjectsModel(8)
	listRegion := regionForTarget(t, m.mouseRegions(m.View().Content), mouseTarget{kind: mouseTargetProjectList})
	wheel := tea.MouseWheelMsg{X: listRegion.bounds.x, Y: listRegion.bounds.y, Button: tea.MouseWheelDown}
	m = dispatchViewMouse(t, m, wheel)
	if selected, _ := m.selectedProjectState(); selected.Name != "project-d" {
		t.Fatalf("wheel selected %q, want project-d", selected.Name)
	}

	content := m.View().Content
	options := regionForTarget(t, m.mouseRegions(content), mouseTarget{kind: mouseTargetButton, action: mouseActionProjectOptions})
	m = clickViewTarget(t, m, options)
	if m.screen != screenProjectOptions || m.adoptName != "project-d" {
		t.Fatalf("options button did not open selected project: screen=%v project=%q", m.screen, m.adoptName)
	}
}

func TestMouseChangesButtonOpensDiffViewer(t *testing.T) {
	m := mouseProjectsModel(2)
	content := m.View().Content
	changes := regionForTarget(t, m.mouseRegions(content), mouseTarget{kind: mouseTargetButton, action: mouseActionReviewChanges})
	m = clickViewTarget(t, m, changes)
	if m.screen != screenSyncDiff || m.syncDiffReturn != screenProjects {
		t.Fatalf("changes button did not open project diff: screen=%v return=%v", m.screen, m.syncDiffReturn)
	}
}

func TestProfileMouseHoverSelectAndOptionsButton(t *testing.T) {
	m := mouseProjectsModel(1)
	m.screen = screenProfiles
	m.selectedProject = "project-a"
	m.profiles = []string{"dev", "staging"}
	m.manifest.Projects["project-a"] = vault.Project{Profiles: map[string]vault.Profile{"dev": {}, "staging": {}}}

	content := m.View().Content
	row := regionForTarget(t, m.mouseRegions(content), mouseTarget{kind: mouseTargetProfileRow, index: 1})
	m = dispatchViewMouse(t, m, tea.MouseMotionMsg{X: row.bounds.x, Y: row.bounds.y})
	if view := ansi.Strip(m.renderProfileList("dev")); !strings.Contains(view, "• staging") {
		t.Fatalf("profile hover has no visual feedback:\n%s", view)
	}
	m = dispatchViewMouse(t, m, tea.MouseClickMsg{X: row.bounds.x, Y: row.bounds.y, Button: tea.MouseLeft})
	if m.profileCursor != 1 {
		t.Fatalf("profile click selected cursor %d", m.profileCursor)
	}

	content = m.View().Content
	options := regionForTarget(t, m.mouseRegions(content), mouseTarget{kind: mouseTargetButton, action: mouseActionProfileOptions})
	m = clickViewTarget(t, m, options)
	if m.screen != screenProjectOptions || m.adoptName != "project-a" {
		t.Fatalf("profile options button failed: screen=%v project=%q", m.screen, m.adoptName)
	}
}

func TestMouseButtonsCancelConfirmationAndFocusFormField(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	confirmation := model{
		cfg:            &cfg,
		screen:         screenConfirmCapture,
		pendingProject: "api",
		pendingProfile: "dev",
		pendingCapture: captureNewProject,
		width:          80,
		height:         24,
	}
	content := confirmation.View().Content
	cancel := regionForTarget(t, confirmation.mouseRegions(content), mouseTarget{kind: mouseTargetButton, action: mouseActionCancel})
	confirmation = clickViewTarget(t, confirmation, cancel)
	if confirmation.screen != screenProjects || confirmation.info != "cancelled" {
		t.Fatalf("cancel button did not use confirmation semantics: screen=%v info=%q", confirmation.screen, confirmation.info)
	}

	form := model{
		cfg:         &cfg,
		screen:      screenAddProject,
		fields:      []field{{label: "Project name", value: "api"}, {label: "Initial profile", value: "dev"}},
		fieldCursor: 0,
		width:       80,
		height:      24,
	}
	content = form.View().Content
	secondField := regionForTarget(t, form.mouseRegions(content), mouseTarget{kind: mouseTargetField, index: 1})
	form = dispatchViewMouse(t, form, tea.MouseClickMsg{X: secondField.bounds.x, Y: secondField.bounds.y, Button: tea.MouseLeft})
	if form.fieldCursor != 1 {
		t.Fatalf("click did not focus second field: cursor=%d", form.fieldCursor)
	}
}
