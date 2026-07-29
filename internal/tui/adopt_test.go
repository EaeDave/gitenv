package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// missingProjectManifest is a vault holding one project with a recorded
// repository and no local link — the fresh-machine situation this wave fixes.
func missingProjectManifest() vault.Manifest {
	return vault.Manifest{Projects: map[string]vault.Project{
		"api": {
			Name:         "api",
			Repositories: []vault.Repository{{Identity: "github.com/acme/api"}},
			Profiles:     map[string]vault.Profile{"dev": {}},
		},
	}}
}

// TestReloadListsUnlinkedVaultProjectAsMissing is the regression test for the
// whole feature: a vault project with no local link must appear (as missing)
// instead of leaving the list empty.
func TestReloadListsUnlinkedVaultProjectAsMissing(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects}

	next, _ := m.Update(reloadMsg{manifest: missingProjectManifest(), statuses: map[string]string{}})
	got := next.(model)

	if len(got.projectStates) != 1 || len(got.projects) != 1 {
		t.Fatalf("unlinked vault project was not listed: states=%#v names=%#v", got.projectStates, got.projects)
	}
	state := got.projectStates[0]
	if state.Name != "api" || state.Kind != app.ProjectMissing {
		t.Fatalf("vault project not classified as missing: %#v", state)
	}
}

// TestFreshMachineAdoptsMissingProject exercises the acceptance path end to end:
// a vault project with no local link renders as adoptable and pressing enter
// reaches a destination form prefilled under the workspace root.
func TestFreshMachineAdoptsMissingProject(t *testing.T) {
	root := t.TempDir()
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}, WorkspaceRoot: root}
	m := model{cfg: &cfg, screen: screenProjects}

	next, _ := m.Update(reloadMsg{manifest: missingProjectManifest(), statuses: map[string]string{}})
	m = next.(model)
	if len(m.projectStates) != 1 || m.projectStates[0].Kind != app.ProjectMissing {
		t.Fatalf("fresh machine did not list the vault project as missing: %#v", m.projectStates)
	}

	next, cmd := m.projectsKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatalf("opening the destination form must not start a command")
	}
	if got.screen != screenAdoptClone || got.adoptName != "api" {
		t.Fatalf("enter did not reach the clone destination form: screen=%v target=%q", got.screen, got.adoptName)
	}
	if len(got.fields) != 1 || !strings.Contains(got.fields[0].value, root) {
		t.Fatalf("destination not prefilled under workspace root %q: %#v", root, got.fields)
	}
	if got.fields[0].value != app.SuggestCloneDest(cfg, "api") {
		t.Fatalf("destination not sourced from SuggestCloneDest: %q", got.fields[0].value)
	}
}

// TestEnterOnFoundProjectRoutesByCandidateCount verifies the found-project fork:
// one candidate prefills the link form, several open the picker.
func TestEnterOnFoundProjectRoutesByCandidateCount(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}

	single := model{cfg: &cfg, screen: screenProjects, projectStates: []app.ProjectState{
		{Name: "api", Kind: app.ProjectFound, Candidates: []string{"/home/me/dev/api"}},
	}}
	single.projects = projectNames(single.projectStates)
	next, _ := single.projectsKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.screen != screenAdoptLink || got.adoptName != "api" {
		t.Fatalf("single candidate did not open the link form: screen=%v", got.screen)
	}
	if len(got.fields) != 1 || got.fields[0].value != "/home/me/dev/api" {
		t.Fatalf("single candidate path not prefilled: %#v", got.fields)
	}

	multi := model{cfg: &cfg, screen: screenProjects, projectStates: []app.ProjectState{
		{Name: "api", Kind: app.ProjectFound, Candidates: []string{"/a/api", "/b/api"}},
	}}
	multi.projects = projectNames(multi.projectStates)
	next, _ = multi.projectsKey(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if got.screen != screenAdoptCandidates || len(got.adoptCandidates) != 2 {
		t.Fatalf("multiple candidates did not open the picker: screen=%v candidates=%#v", got.screen, got.adoptCandidates)
	}
}

// TestCaptureOnNonLinkedProjectDoesNotStartOperation guards the capture key on a
// selection that has no local path: it must explain, not fail deeper.
func TestCaptureOnNonLinkedProjectDoesNotStartOperation(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects, projectStates: []app.ProjectState{
		{Name: "api", Kind: app.ProjectMissing, Identity: "github.com/acme/api"},
	}}
	m.projects = projectNames(m.projectStates)

	next, cmd := m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := next.(model)
	if cmd != nil || got.busy {
		t.Fatalf("capture on non-linked project started an operation: cmd=%v busy=%v", cmd, got.busy)
	}
	if got.screen != screenProjects || got.errText == "" {
		t.Fatalf("capture on non-linked project should set an error and stay put: screen=%v err=%q", got.screen, got.errText)
	}
}

// TestRenderProjectListShowsBadgeAndIdentityForMissing asserts on the rendered
// row, the way the existing view tests do.
func TestRenderProjectListShowsBadgeAndIdentityForMissing(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects, projectStates: []app.ProjectState{
		{Name: "api", Kind: app.ProjectMissing, Identity: "github.com/acme/api"},
	}}
	m.projects = projectNames(m.projectStates)

	out := m.renderProjectList()
	if !strings.Contains(out, "◌") {
		t.Fatalf("missing project badge not rendered:\n%s", out)
	}
	if !strings.Contains(out, "github.com/acme/api") {
		t.Fatalf("missing project repository identity not rendered:\n%s", out)
	}
}

// TestProjectListDistinguishesEmptyFromUnlinked verifies the two empty states:
// a truly empty vault versus one whose projects exist but are not linked here.
func TestProjectListDistinguishesEmptyFromUnlinked(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}

	empty := model{cfg: &cfg, screen: screenProjects}
	if out := empty.renderProjectList(); !strings.Contains(out, "No projects in the vault yet") {
		t.Fatalf("empty vault did not use the empty-vault message:\n%s", out)
	}

	unlinked := model{cfg: &cfg, screen: screenProjects, projectStates: []app.ProjectState{
		{Name: "api", Kind: app.ProjectMissing, Identity: "github.com/acme/api"},
	}}
	unlinked.projects = projectNames(unlinked.projectStates)
	out := unlinked.renderProjectList()
	if strings.Contains(out, "No projects in the vault yet") {
		t.Fatalf("unlinked case wrongly used the empty-vault message:\n%s", out)
	}
	if !strings.Contains(out, "press enter to adopt") || !strings.Contains(out, "d to scan") {
		t.Fatalf("unlinked case did not guide the user to adopt or scan:\n%s", out)
	}
}

// TestAdoptCandidatesPickerLinksSelection verifies the picker links the chosen
// clone via a command without blocking.
func TestAdoptCandidatesPickerLinksSelection(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{
		cfg:             &cfg,
		screen:          screenAdoptCandidates,
		adoptName:       "api",
		adoptCandidates: []string{"/a/api", "/b/api"},
		menuCursor:      1,
	}
	next, cmd := m.adoptCandidatesKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd == nil || !got.busy {
		t.Fatalf("selecting a candidate did not start the link command: cmd=%v busy=%v", cmd, got.busy)
	}
	if got.adoptPath != "/b/api" {
		t.Fatalf("selected candidate path not recorded: %q", got.adoptPath)
	}
}

// TestAdoptSuccessReportsMethodAndReloads verifies a successful clone-adopt sets
// an informative message naming the method and triggers a reload.
func TestAdoptSuccessReportsMethodAndReloads(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenAdoptClone, adoptName: "api", adoptPath: "/home/me/dev/api", busy: true}

	next, cmd := m.Update(adoptMsg{project: "api", outcome: gitops.CloneOutcome{Method: gitops.CloneMethodGH, URL: "github.com/acme/api"}})
	got := next.(model)
	if got.busy || got.screen != screenProjects {
		t.Fatalf("successful adopt did not settle back on the project list: busy=%v screen=%v", got.busy, got.screen)
	}
	if cmd == nil {
		t.Fatalf("successful adopt did not trigger a reload")
	}
	if !strings.Contains(got.info, "gh") || !strings.Contains(got.info, "/home/me/dev/api") {
		t.Fatalf("adopt result did not name the method and destination: %q", got.info)
	}
}

// TestAdoptCloneSucceededButLinkFailedIsExplained verifies the partial-failure
// path: the clone landed but adopting did not, so say the repo is on disk and
// still reload because the link may have persisted.
func TestAdoptCloneSucceededButLinkFailedIsExplained(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenAdoptClone, adoptName: "api", adoptPath: "/home/me/dev/api", busy: true}

	next, cmd := m.Update(adoptMsg{
		project: "api",
		outcome: gitops.CloneOutcome{Method: gitops.CloneMethodGit, URL: "github.com/acme/api"},
		err:     errors.New("uncaptured .env content"),
	})
	got := next.(model)
	if cmd == nil {
		t.Fatalf("partial failure must still reload (the link may have persisted)")
	}
	if got.errText == "" || !strings.Contains(got.errText, "on disk") {
		t.Fatalf("partial failure did not explain the repository is on disk: %q", got.errText)
	}
}
